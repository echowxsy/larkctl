package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
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

type agentRunner interface {
	Run(ctx context.Context, prompt, sessionID string) (agentResult, error)
}

type botReplier interface {
	BotReply(ctx context.Context, chatID, text, replyToMessageID string) error
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
	cwd            string
	permissionMode string
	model          string
	turnTimeout    time.Duration
}

func (r *claudeCLIRunner) Run(ctx context.Context, prompt, sessionID string) (agentResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.turnTimeout)
	defer cancel()

	args := []string{"-p", "--output-format", "stream-json", "--verbose",
		"--permission-mode", r.permissionMode}
	if sessionID != "" {
		args = append(args, "--resume", sessionID)
	}
	if r.model != "" {
		args = append(args, "--model", r.model)
	}

	cmd := exec.CommandContext(ctx, r.binary, args...)
	cmd.Dir = r.cwd
	cmd.Stdin = strings.NewReader(prompt)
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
}

// parseIncomingMessage extracts a text message from a routed schema-2.0
// envelope; ok=false for anything that is not a text message event.
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
	if env.Header.EventType != "im.message.receive_v1" || m.MessageType != "text" || m.ChatID == "" {
		return incomingMessage{}, false
	}
	var content struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(m.Content), &content); err != nil {
		return incomingMessage{}, false
	}
	text := strings.TrimSpace(content.Text)
	if text == "" {
		return incomingMessage{}, false
	}
	return incomingMessage{
		ChatID:       m.ChatID,
		ChatType:     m.ChatType,
		MessageID:    m.MessageID,
		Text:         text,
		SenderOpenID: env.Event.Sender.SenderID.OpenID,
	}, true
}

// agentSessions maps chat_id → claude session_id.
type agentSessions struct {
	mu sync.Mutex
	m  map[string]string
}

func newAgentSessions() *agentSessions { return &agentSessions{m: map[string]string{}} }

func (s *agentSessions) get(chatID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m[chatID]
}

func (s *agentSessions) set(chatID, sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sessionID == "" {
		delete(s.m, chatID)
		return
	}
	s.m[chatID] = sessionID
}

func truncateReply(text string) string {
	runes := []rune(text)
	if len(runes) <= agentReplyMaxRunes {
		return text
	}
	return string(runes[:agentReplyMaxRunes]) + "\n…[truncated]"
}

// agentHandleMessage executes one turn: slash commands, claude run, reply.
func agentHandleMessage(ctx context.Context, runner agentRunner, replier botReplier,
	sessions *agentSessions, msg incomingMessage,
) {
	reply := func(text string) {
		if err := replier.BotReply(ctx, msg.ChatID, truncateReply(text), msg.MessageID); err != nil {
			log.Printf("agent: reply to %s failed: %v", msg.ChatID, err)
		}
	}

	switch strings.TrimSpace(msg.Text) {
	case "/new", "/reset":
		sessions.set(msg.ChatID, "")
		reply("🔄 会话已重置")
		return
	}

	res, err := runner.Run(ctx, msg.Text, sessions.get(msg.ChatID))
	if res.SessionID != "" {
		sessions.set(msg.ChatID, res.SessionID)
	}
	if err != nil {
		reply("⚠️ agent 运行失败: " + err.Error())
		return
	}
	if res.Text == "" {
		reply("⚠️ agent 没有返回内容")
		return
	}
	reply(res.Text)
}

// agentServe pumps the event stream into per-chat serial workers.
func agentServe(ctx context.Context, gc *GatewayClient, runner agentRunner, logf func(string, ...any)) error {
	sessions := newAgentSessions()
	var mu sync.Mutex
	workers := map[string]chan incomingMessage{}

	workerFor := func(chatID string) chan incomingMessage {
		mu.Lock()
		defer mu.Unlock()
		if ch, ok := workers[chatID]; ok {
			return ch
		}
		ch := make(chan incomingMessage, agentQueueDepth)
		workers[chatID] = ch
		go func() {
			for msg := range ch {
				logf("agent: [%s] ← %q", msg.ChatID, truncateForLog(msg.Text))
				agentHandleMessage(ctx, runner, gc, sessions, msg)
				logf("agent: [%s] turn done", msg.ChatID)
			}
		}()
		return ch
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
				select {
				case workerFor(msg.ChatID) <- msg:
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
		Long: "Listens on the gateway event stream, feeds each of your text messages to\n" +
			"the local `claude` CLI (one session per chat, resumed across turns), and\n" +
			"replies in the chat as the bot. Send /new in a chat to reset its session.",
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

			runner := &claudeCLIRunner{
				binary:         binary,
				cwd:            workspace,
				permissionMode: mode,
				model:          model,
				turnTimeout:    time.Duration(timeoutSec) * time.Second,
			}
			logf := func(format string, a ...any) {
				fmt.Fprintf(os.Stderr, format+"\n", a...)
			}
			logf("agent: workspace=%s permission-mode=%s claude=%s", workspace, mode, binary)
			return agentServe(cmd.Context(), gc, runner, logf)
		},
	}
	serveCmd.Flags().String("claude-bin", "claude", "claude CLI binary")
	serveCmd.Flags().String("workspace", ".", "working directory for agent runs")
	serveCmd.Flags().String("permission-mode", "bypassPermissions", "claude permission mode (default|acceptEdits|plan|bypassPermissions)")
	serveCmd.Flags().String("model", "", "claude model override")
	serveCmd.Flags().Int("turn-timeout", 600, "per-turn timeout in seconds")

	agentCmd.AddCommand(serveCmd)
	return agentCmd
}
