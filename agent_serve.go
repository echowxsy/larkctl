package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
)

// larkctl agent serve — drive a local Claude Code from your Feishu messages.
//
// Consumes the gateway's per-user event stream (same source as `im listen`),
// runs each text message through the local `claude` CLI (one Claude session
// per chat, resumed across turns), and posts the final answer back into the
// chat through the gateway's bot-reply endpoint.
//
// Feature parity with lark-channel-bridge (tier 1): group @-mentions, post
// (rich text) flattening, image/file download into the workspace, merge of
// messages queued while a turn is running, /stop interruption, per-chat
// working directories (/cd, /ws) and /status, /help.
//
// The claude invocation mirrors lark-channel-bridge's ClaudeAdapter:
// `claude -p --output-format stream-json --verbose --permission-mode <mode>
// [--resume <session>]` with the prompt on stdin.

const (
	agentReplyMaxRunes = 20000
	agentQueueDepth    = 16
)

type agentResult struct {
	Text      string
	SessionID string
	IsError   bool
}

type agentRunReq struct {
	Prompt    string
	SessionID string
	Cwd       string
}

type agentRunner interface {
	Run(ctx context.Context, req agentRunReq) (agentResult, error)
}

type botReplier interface {
	BotReply(ctx context.Context, chatID, text, replyToMessageID string) error
}

type mediaDownloader interface {
	DownloadMessageResource(ctx context.Context, messageID, fileKey, resourceType string, w io.Writer) error
}

// parseClaudeStream reads claude's stream-json output (JSONL) and returns the
// final result event's text plus the session id for later --resume.
func parseClaudeStream(r io.Reader) (agentResult, error) {
	var res agentResult
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024*1024), 32*1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var evt struct {
			Type      string `json:"type"`
			Subtype   string `json:"subtype"`
			SessionID string `json:"session_id"`
			Result    string `json:"result"`
			IsError   bool   `json:"is_error"`
		}
		if err := json.Unmarshal(line, &evt); err != nil {
			continue // non-JSON noise on stdout is ignorable
		}
		if evt.SessionID != "" {
			res.SessionID = evt.SessionID
		}
		if evt.Type == "result" {
			res.Text = evt.Result
			res.IsError = evt.IsError
		}
	}
	return res, scanner.Err()
}

// claudeCLIRunner spawns the local claude binary once per turn.
type claudeCLIRunner struct {
	binary         string
	permissionMode string
	model          string
	turnTimeout    time.Duration
}

func (r *claudeCLIRunner) Run(ctx context.Context, req agentRunReq) (agentResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.turnTimeout)
	defer cancel()

	args := []string{"-p", "--output-format", "stream-json", "--verbose",
		"--permission-mode", r.permissionMode}
	if req.SessionID != "" {
		args = append(args, "--resume", req.SessionID)
	}
	if r.model != "" {
		args = append(args, "--model", r.model)
	}

	cmd := exec.CommandContext(ctx, r.binary, args...)
	cmd.Dir = req.Cwd
	cmd.Stdin = strings.NewReader(req.Prompt)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return agentResult{}, err
	}
	if err := cmd.Start(); err != nil {
		return agentResult{}, fmt.Errorf("start %s: %w", r.binary, err)
	}
	res, parseErr := parseClaudeStream(stdout)
	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return res, ctx.Err()
	}
	if waitErr != nil && res.Text == "" {
		detail := strings.TrimSpace(stderr.String())
		if len(detail) > 500 {
			detail = detail[len(detail)-500:]
		}
		return res, fmt.Errorf("claude failed: %v: %s", waitErr, detail)
	}
	if parseErr != nil && res.Text == "" {
		return res, parseErr
	}
	return res, nil
}

// incomingMessage is the routed Feishu message an agent turn consumes.
type incomingMessage struct {
	ChatID       string
	ChatType     string
	MessageID    string
	Text         string
	SenderOpenID string

	// Media payloads (image / file messages)
	MediaType string // "image" | "file"
	MediaKey  string
	MediaName string
}

// parseIncomingMessage extracts an agent-consumable message from a routed
// schema-2.0 envelope. Supported message types: text (with @-mention
// placeholders resolved to @name), post (flattened to text), image, file.
func parseIncomingMessage(data []byte) (incomingMessage, bool) {
	var env struct {
		Event struct {
			Sender struct {
				SenderID struct {
					OpenID string `json:"open_id"`
				} `json:"sender_id"`
			} `json:"sender"`
			Message struct {
				MessageID   string `json:"message_id"`
				ChatID      string `json:"chat_id"`
				ChatType    string `json:"chat_type"`
				MessageType string `json:"message_type"`
				Content     string `json:"content"`
				Mentions    []struct {
					Key  string `json:"key"`
					Name string `json:"name"`
				} `json:"mentions"`
			} `json:"message"`
		} `json:"event"`
		Header struct {
			EventType string `json:"event_type"`
		} `json:"header"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return incomingMessage{}, false
	}
	m := env.Event.Message
	if env.Header.EventType != "im.message.receive_v1" || m.ChatID == "" {
		return incomingMessage{}, false
	}

	msg := incomingMessage{
		ChatID:       m.ChatID,
		ChatType:     m.ChatType,
		MessageID:    m.MessageID,
		SenderOpenID: env.Event.Sender.SenderID.OpenID,
	}

	switch m.MessageType {
	case "text":
		var content struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(m.Content), &content); err != nil {
			return incomingMessage{}, false
		}
		text := content.Text
		for _, mention := range m.Mentions {
			if mention.Key != "" {
				text = strings.ReplaceAll(text, mention.Key, "@"+mention.Name)
			}
		}
		msg.Text = strings.TrimSpace(text)
	case "post":
		var content struct {
			Title   string `json:"title"`
			Content [][]struct {
				Tag  string `json:"tag"`
				Text string `json:"text"`
				Href string `json:"href"`
				Name string `json:"user_name"`
			} `json:"content"`
		}
		if err := json.Unmarshal([]byte(m.Content), &content); err != nil {
			return incomingMessage{}, false
		}
		var b strings.Builder
		if content.Title != "" {
			b.WriteString(content.Title + "\n")
		}
		for _, line := range content.Content {
			for _, run := range line {
				switch run.Tag {
				case "a":
					fmt.Fprintf(&b, "%s (%s)", run.Text, run.Href)
				case "at":
					b.WriteString("@" + run.Name)
				default:
					b.WriteString(run.Text)
				}
			}
			b.WriteString("\n")
		}
		msg.Text = strings.TrimSpace(b.String())
	case "image":
		var content struct {
			ImageKey string `json:"image_key"`
		}
		if err := json.Unmarshal([]byte(m.Content), &content); err != nil || content.ImageKey == "" {
			return incomingMessage{}, false
		}
		msg.MediaType = "image"
		msg.MediaKey = content.ImageKey
	case "file":
		var content struct {
			FileKey  string `json:"file_key"`
			FileName string `json:"file_name"`
		}
		if err := json.Unmarshal([]byte(m.Content), &content); err != nil || content.FileKey == "" {
			return incomingMessage{}, false
		}
		msg.MediaType = "file"
		msg.MediaKey = content.FileKey
		msg.MediaName = content.FileName
	default:
		return incomingMessage{}, false
	}

	if msg.Text == "" && msg.MediaKey == "" {
		return incomingMessage{}, false
	}
	return msg, true
}

// isAgentCommand reports whether text is one of the agent slash commands
// (an unknown "/..." string is treated as a normal prompt, so paths like
// "/etc/hosts 是什么" still reach the agent).
func isAgentCommand(text string) bool {
	t := strings.TrimSpace(text)
	for _, c := range []string{"/new", "/reset", "/stop", "/cd", "/ws", "/status", "/help"} {
		if t == c || strings.HasPrefix(t, c+" ") {
			return true
		}
	}
	return false
}

// isInterruptingCommand lists the commands that abort a running turn before
// being processed (mirrors the bridge's interrupt set).
func isInterruptingCommand(text string) bool {
	t := strings.TrimSpace(text)
	return t == "/new" || t == "/reset" || strings.HasPrefix(t, "/cd ") || strings.HasPrefix(t, "/ws use ")
}

// mergeMessageGroups merges consecutive plain text messages (sent while the
// agent was busy) into single turns; commands and media stay standalone.
func mergeMessageGroups(msgs []incomingMessage) []incomingMessage {
	var groups []incomingMessage
	var pending *incomingMessage
	flush := func() {
		if pending != nil {
			groups = append(groups, *pending)
			pending = nil
		}
	}
	for _, m := range msgs {
		if m.MediaKey != "" || isAgentCommand(m.Text) {
			flush()
			groups = append(groups, m)
			continue
		}
		if pending == nil {
			mm := m
			pending = &mm
			continue
		}
		pending.Text += "\n\n" + m.Text
		pending.MessageID = m.MessageID
	}
	flush()
	return groups
}

// resolveMedia downloads a message's image/file into <cwd>/media and rewrites
// the message into a text prompt referencing the saved path.
func resolveMedia(ctx context.Context, dl mediaDownloader, msg incomingMessage, cwd string) (incomingMessage, error) {
	name := filepath.Base(strings.TrimSpace(msg.MediaName))
	if name == "" || name == "." || name == "/" {
		ext := ".bin"
		if msg.MediaType == "image" {
			ext = ".png"
		}
		name = msg.MessageID + ext
	}
	dir := filepath.Join(cwd, "media")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return msg, err
	}
	path := filepath.Join(dir, name)
	f, err := os.Create(path) //nolint:gosec // path is under the chat workspace
	if err != nil {
		return msg, err
	}
	defer f.Close()
	if err := dl.DownloadMessageResource(ctx, msg.MessageID, msg.MediaKey, msg.MediaType, f); err != nil {
		os.Remove(path)
		return msg, err
	}

	kind := "文件"
	if msg.MediaType == "image" {
		kind = "图片"
	}
	msg.Text = fmt.Sprintf("用户发来%s，已保存到 %s ，请按需查看。", kind, path)
	msg.MediaKey = ""
	return msg, nil
}

func truncateReply(text string) string {
	runes := []rune(text)
	if len(runes) <= agentReplyMaxRunes {
		return text
	}
	return string(runes[:agentReplyMaxRunes]) + "\n…[truncated]"
}

const agentHelpText = `🤖 agent 命令
/new 或 /reset — 重置当前会话
/stop — 停止正在运行的任务
/cd <绝对路径> — 切换工作目录（会重置会话）
/ws list — 列出命名工作区
/ws save <名字> — 把当前目录存为命名工作区
/ws use <名字> — 切换到命名工作区
/ws remove <名字> — 删除命名工作区
/status — 查看当前状态
/help — 本帮助
其他消息会交给本地 Claude Code 处理；图片/文件会下载到工作区 media/ 目录。`

// agentHandleMessage executes one turn: slash commands, claude run, reply.
func agentHandleMessage(ctx context.Context, runner agentRunner, replier botReplier,
	state *agentState, msg incomingMessage,
) {
	reply := func(text string) {
		if err := replier.BotReply(ctx, msg.ChatID, truncateReply(text), msg.MessageID); err != nil {
			log.Printf("agent: reply to %s failed: %v", msg.ChatID, err)
		}
	}

	trimmed := strings.TrimSpace(msg.Text)
	switch {
	case trimmed == "/new" || trimmed == "/reset":
		state.setSession(msg.ChatID, "")
		reply("🔄 会话已重置")
		return
	case trimmed == "/stop":
		// A running turn is interrupted upstream (worker); reaching here
		// means the agent was idle.
		reply("当前没有正在运行的任务")
		return
	case strings.HasPrefix(trimmed, "/cd ") || trimmed == "/cd":
		dir := strings.TrimSpace(strings.TrimPrefix(trimmed, "/cd"))
		if dir == "" {
			reply("用法: /cd <绝对路径>")
			return
		}
		if err := state.setCwd(msg.ChatID, dir); err != nil {
			reply("⚠️ " + err.Error())
			return
		}
		reply("📂 已切换到 " + dir + " ，会话已重置")
		return
	case strings.HasPrefix(trimmed, "/ws"):
		reply(handleWorkspaceCommand(state, msg.ChatID, trimmed))
		return
	case trimmed == "/status":
		c := state.chat(msg.ChatID)
		session := c.Session
		if session == "" {
			session = "（新会话）"
		}
		reply(fmt.Sprintf("📊 状态\n工作目录: %s\n会话: %s\n命名工作区: %d 个",
			c.Cwd, session, len(state.wsList())))
		return
	case trimmed == "/help":
		reply(agentHelpText)
		return
	}

	c := state.chat(msg.ChatID)
	res, err := runner.Run(ctx, agentRunReq{Prompt: msg.Text, SessionID: c.Session, Cwd: c.Cwd})
	if res.SessionID != "" {
		state.setSession(msg.ChatID, res.SessionID)
	}
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return // interrupted via /stop; the interrupt already answered
		}
		reply("⚠️ agent 运行失败: " + err.Error())
		return
	}
	if res.Text == "" {
		reply("⚠️ agent 没有返回内容")
		return
	}
	reply(res.Text)
}

func handleWorkspaceCommand(state *agentState, chatID, trimmed string) string {
	fields := strings.Fields(trimmed)
	sub := ""
	if len(fields) > 1 {
		sub = fields[1]
	}
	arg := ""
	if len(fields) > 2 {
		arg = strings.Join(fields[2:], " ")
	}
	switch sub {
	case "list":
		ws := state.wsList()
		if len(ws) == 0 {
			return "（还没有命名工作区，/ws save <名字> 保存当前目录）"
		}
		names := make([]string, 0, len(ws))
		for name := range ws {
			names = append(names, name)
		}
		sort.Strings(names)
		var b strings.Builder
		b.WriteString("📁 命名工作区\n")
		for _, name := range names {
			fmt.Fprintf(&b, "%s → %s\n", name, ws[name])
		}
		return strings.TrimSpace(b.String())
	case "save":
		if arg == "" {
			return "用法: /ws save <名字>"
		}
		cwd := state.chat(chatID).Cwd
		if err := state.wsSave(arg, cwd); err != nil {
			return "⚠️ " + err.Error()
		}
		return fmt.Sprintf("💾 已保存工作区 %s → %s", arg, cwd)
	case "use":
		if arg == "" {
			return "用法: /ws use <名字>"
		}
		path, ok := state.wsPath(arg)
		if !ok {
			return "⚠️ 工作区不存在: " + arg
		}
		if err := state.setCwd(chatID, path); err != nil {
			return "⚠️ " + err.Error()
		}
		return "📂 已切换到 " + path + " ，会话已重置"
	case "remove":
		if arg == "" {
			return "用法: /ws remove <名字>"
		}
		if err := state.wsRemove(arg); err != nil {
			return "⚠️ " + err.Error()
		}
		return "🗑 已删除工作区 " + arg
	default:
		return "用法: /ws list | save <名字> | use <名字> | remove <名字>"
	}
}

// chatWorker serializes turns for one chat and tracks the in-flight run so
// control commands can interrupt it.
type chatWorker struct {
	ch        chan incomingMessage
	mu        sync.Mutex
	runCancel context.CancelFunc
}

func (w *chatWorker) setCancel(c context.CancelFunc) {
	w.mu.Lock()
	w.runCancel = c
	w.mu.Unlock()
}

// interrupt cancels the in-flight run, if any.
func (w *chatWorker) interrupt() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.runCancel == nil {
		return false
	}
	w.runCancel()
	w.runCancel = nil
	return true
}

// agentServe pumps the event stream into per-chat serial workers.
func agentServe(ctx context.Context, gc *GatewayClient, runner agentRunner,
	state *agentState, logf func(string, ...any),
) error {
	var mu sync.Mutex
	workers := map[string]*chatWorker{}

	workerFor := func(chatID string) *chatWorker {
		mu.Lock()
		defer mu.Unlock()
		if w, ok := workers[chatID]; ok {
			return w
		}
		w := &chatWorker{ch: make(chan incomingMessage, agentQueueDepth)}
		workers[chatID] = w
		go func() {
			for msg := range w.ch {
				batch := []incomingMessage{msg}
			drain:
				for {
					select {
					case m2 := <-w.ch:
						batch = append(batch, m2)
					default:
						break drain
					}
				}
				for _, g := range mergeMessageGroups(batch) {
					if g.MediaKey != "" {
						resolved, err := resolveMedia(ctx, gc, g, state.chat(g.ChatID).Cwd)
						if err != nil {
							logf("agent: [%s] media download failed: %v", g.ChatID, err)
							if rerr := gc.BotReply(ctx, g.ChatID, "⚠️ 附件下载失败: "+err.Error(), g.MessageID); rerr != nil {
								logf("agent: [%s] reply failed: %v", g.ChatID, rerr)
							}
							continue
						}
						g = resolved
					}
					logf("agent: [%s] ← %q", g.ChatID, truncateForLog(g.Text))
					runCtx, cancel := context.WithCancel(ctx)
					w.setCancel(cancel)
					agentHandleMessage(runCtx, runner, gc, state, g)
					w.setCancel(nil)
					cancel()
					logf("agent: [%s] turn done", g.ChatID)
				}
			}
		}()
		return w
	}

	for {
		superseded := false
		err := gc.StreamEvents(ctx, func(event, data string) {
			switch event {
			case "ready":
				logf("agent: listening %s", data)
			case "superseded":
				superseded = true
			default:
				msg, ok := parseIncomingMessage([]byte(data))
				if !ok {
					return
				}
				w := workerFor(msg.ChatID)
				trimmed := strings.TrimSpace(msg.Text)
				if trimmed == "/stop" {
					if w.interrupt() {
						go func() {
							if err := gc.BotReply(ctx, msg.ChatID, "⏹ 已停止当前任务", msg.MessageID); err != nil {
								logf("agent: [%s] reply failed: %v", msg.ChatID, err)
							}
						}()
						return
					}
					// idle: fall through to the queue so the worker answers
				} else if isInterruptingCommand(trimmed) {
					w.interrupt()
				}
				select {
				case w.ch <- msg:
				default:
					logf("agent: [%s] queue full, dropping message", msg.ChatID)
				}
			}
		})
		if ctx.Err() != nil {
			return nil
		}
		if superseded {
			return fmt.Errorf("event stream taken over by a newer agent/listener for this account")
		}
		logf("agent: stream ended (%v), reconnecting in 5s...", err)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(5 * time.Second):
		}
	}
}

func truncateForLog(s string) string {
	runes := []rune(s)
	if len(runes) > 80 {
		return string(runes[:80]) + "…"
	}
	return s
}

func newAgentCmd() *cobra.Command {
	agentCmd := &cobra.Command{
		Use:   "agent",
		Short: "Drive a local coding agent from your Feishu messages",
	}

	serveCmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the agent loop: Feishu message → local claude → bot reply",
		Long: "Listens on the gateway event stream, feeds each of your messages to the\n" +
			"local `claude` CLI (one session per chat, resumed across turns), and\n" +
			"replies in the chat as the bot. Send /help in a chat for commands.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			gc, ok := client.(*GatewayClient)
			if !ok {
				return fmt.Errorf("agent serve requires gateway mode")
			}

			binary, _ := cmd.Flags().GetString("claude-bin")
			workspace, _ := cmd.Flags().GetString("workspace")
			mode, _ := cmd.Flags().GetString("permission-mode")
			model, _ := cmd.Flags().GetString("model")
			timeoutSec, _ := cmd.Flags().GetInt("turn-timeout")

			if _, err := exec.LookPath(binary); err != nil {
				return fmt.Errorf("claude binary %q not found in PATH", binary)
			}
			switch mode {
			case "default", "acceptEdits", "plan", "bypassPermissions":
			default:
				return fmt.Errorf("invalid --permission-mode %q (default|acceptEdits|plan|bypassPermissions)", mode)
			}
			if mode == "bypassPermissions" {
				fmt.Fprintln(os.Stderr, "⚠️  permission mode bypassPermissions: claude runs without permission prompts")
			}

			absWorkspace, err := filepath.Abs(workspace)
			if err != nil {
				return err
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			state, err := newAgentState(absWorkspace, filepath.Join(home, ".lark", "agent_workspaces.json"))
			if err != nil {
				return err
			}

			runner := &claudeCLIRunner{
				binary:         binary,
				permissionMode: mode,
				model:          model,
				turnTimeout:    time.Duration(timeoutSec) * time.Second,
			}
			logf := func(format string, a ...any) {
				fmt.Fprintf(os.Stderr, format+"\n", a...)
			}
			logf("agent: workspace=%s permission-mode=%s claude=%s", absWorkspace, mode, binary)
			return agentServe(cmd.Context(), gc, runner, state, logf)
		},
	}
	serveCmd.Flags().String("claude-bin", "claude", "claude CLI binary")
	serveCmd.Flags().String("workspace", ".", "default working directory for agent runs")
	serveCmd.Flags().String("permission-mode", "bypassPermissions", "claude permission mode (default|acceptEdits|plan|bypassPermissions)")
	serveCmd.Flags().String("model", "", "claude model override")
	serveCmd.Flags().Int("turn-timeout", 600, "per-turn timeout in seconds")

	agentCmd.AddCommand(serveCmd)
	return agentCmd
}
