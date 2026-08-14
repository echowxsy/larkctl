package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestAgentState builds an agentState with a temp default workspace and a
// temp workspaces file.
func newTestAgentState(t *testing.T) *agentState {
	t.Helper()
	dir := t.TempDir()
	st, err := newAgentState(dir, filepath.Join(dir, "ws.json"))
	if err != nil {
		t.Fatalf("newAgentState: %v", err)
	}
	return st
}

func envelopeWith(t *testing.T, msgType, contentJSON, mentionsJSON string) []byte {
	t.Helper()
	mentions := ""
	if mentionsJSON != "" {
		mentions = `,"mentions":` + mentionsJSON
	}
	env := `{"event":{"sender":{"sender_id":{"open_id":"ou_abc"}},` +
		`"message":{"message_id":"om_1","chat_id":"oc_1","chat_type":"group",` +
		`"message_type":"` + msgType + `","content":` + contentJSON + mentions + `}},` +
		`"header":{"event_type":"im.message.receive_v1"},"schema":"2.0"}`
	return []byte(env)
}

func TestParseIncomingGroupMentionsReplaced(t *testing.T) {
	content := `"{\"text\":\"@_user_1 帮我查一下 @_user_2 的进度\"}"`
	mentions := `[{"key":"@_user_1","id":{"open_id":"ou_bot"},"name":"larkctl"},` +
		`{"key":"@_user_2","id":{"open_id":"ou_x"},"name":"张三"}]`
	msg, ok := parseIncomingMessage(envelopeWith(t, "text", content, mentions))
	if !ok {
		t.Fatal("group text message not recognized")
	}
	if msg.Text != "@larkctl 帮我查一下 @张三 的进度" {
		t.Fatalf("text = %q", msg.Text)
	}
}

func TestParseIncomingPost(t *testing.T) {
	// post content: title + two paragraphs with text/a/at runs
	post := `"{\"title\":\"需求\",\"content\":[[{\"tag\":\"text\",\"text\":\"第一行 \"},` +
		`{\"tag\":\"a\",\"text\":\"链接\",\"href\":\"https://example.com\"}],` +
		`[{\"tag\":\"text\",\"text\":\"第二行\"}]]}"`
	msg, ok := parseIncomingMessage(envelopeWith(t, "post", post, ""))
	if !ok {
		t.Fatal("post message not recognized")
	}
	for _, want := range []string{"需求", "第一行", "链接", "https://example.com", "第二行"} {
		if !strings.Contains(msg.Text, want) {
			t.Fatalf("post text %q missing %q", msg.Text, want)
		}
	}
}

func TestParseIncomingImage(t *testing.T) {
	msg, ok := parseIncomingMessage(envelopeWith(t, "image", `"{\"image_key\":\"img_k1\"}"`, ""))
	if !ok {
		t.Fatal("image message not recognized")
	}
	if msg.MediaKey != "img_k1" || msg.MediaType != "image" {
		t.Fatalf("msg = %+v", msg)
	}
}

func TestParseIncomingFile(t *testing.T) {
	msg, ok := parseIncomingMessage(envelopeWith(t, "file", `"{\"file_key\":\"file_k1\",\"file_name\":\"report.pdf\"}"`, ""))
	if !ok {
		t.Fatal("file message not recognized")
	}
	if msg.MediaKey != "file_k1" || msg.MediaType != "file" || msg.MediaName != "report.pdf" {
		t.Fatalf("msg = %+v", msg)
	}
}

func TestMergeMessageGroups(t *testing.T) {
	msgs := []incomingMessage{
		{ChatID: "oc_1", MessageID: "m1", Text: "first"},
		{ChatID: "oc_1", MessageID: "m2", Text: "second"},
		{ChatID: "oc_1", MessageID: "m3", Text: "/cd /tmp"},
		{ChatID: "oc_1", MessageID: "m4", Text: "third"},
	}
	groups := mergeMessageGroups(msgs)
	if len(groups) != 3 {
		t.Fatalf("groups = %+v", groups)
	}
	if groups[0].Text != "first\n\nsecond" || groups[0].MessageID != "m2" {
		t.Fatalf("merged group = %+v", groups[0])
	}
	if groups[1].Text != "/cd /tmp" || groups[2].Text != "third" {
		t.Fatalf("groups = %+v", groups)
	}
}

func TestAgentStateWorkspacesPersist(t *testing.T) {
	dir := t.TempDir()
	wsFile := filepath.Join(dir, "ws.json")
	st, err := newAgentState(dir, wsFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.wsSave("proj", dir); err != nil {
		t.Fatalf("wsSave: %v", err)
	}

	st2, err := newAgentState(dir, wsFile)
	if err != nil {
		t.Fatal(err)
	}
	names := st2.wsList()
	if len(names) != 1 || names["proj"] != dir {
		t.Fatalf("persisted ws = %v", names)
	}
	if err := st2.wsRemove("proj"); err != nil {
		t.Fatalf("wsRemove: %v", err)
	}
	if len(st2.wsList()) != 0 {
		t.Fatal("remove not applied")
	}
}

func TestAgentHandleCdSwitchesAndResets(t *testing.T) {
	runner := &fakeRunner{}
	replier := &fakeReplier{}
	state := newTestAgentState(t)
	state.setSession("oc_1", "sess-old")
	target := t.TempDir()

	agentHandleMessage(context.Background(), runner, replier, state,
		incomingMessage{ChatID: "oc_1", Text: "/cd " + target})

	c := state.chat("oc_1")
	if c.Cwd != target {
		t.Fatalf("cwd = %q, want %q", c.Cwd, target)
	}
	if c.Session != "" {
		t.Fatal("session must reset on /cd")
	}
	if len(replier.replies) != 1 || !strings.Contains(replier.replies[0], target) {
		t.Fatalf("replies = %v", replier.replies)
	}
}

func TestAgentHandleCdRejectsBadPath(t *testing.T) {
	runner := &fakeRunner{}
	replier := &fakeReplier{}
	state := newTestAgentState(t)

	agentHandleMessage(context.Background(), runner, replier, state,
		incomingMessage{ChatID: "oc_1", Text: "/cd /definitely/not/a/dir"})

	if len(runner.calls) != 0 {
		t.Fatal("bad /cd must not run agent")
	}
	if len(replier.replies) != 1 || !strings.Contains(replier.replies[0], "⚠️") {
		t.Fatalf("replies = %v", replier.replies)
	}
}

func TestAgentHandleWorkspaceCommands(t *testing.T) {
	runner := &fakeRunner{}
	replier := &fakeReplier{}
	state := newTestAgentState(t)
	dir := t.TempDir()

	run := func(text string) string {
		agentHandleMessage(context.Background(), runner, replier, state,
			incomingMessage{ChatID: "oc_1", Text: text})
		return replier.replies[len(replier.replies)-1]
	}

	run("/cd " + dir)
	if r := run("/ws save proj"); !strings.Contains(r, "proj") {
		t.Fatalf("ws save reply: %q", r)
	}
	if r := run("/ws list"); !strings.Contains(r, "proj") || !strings.Contains(r, dir) {
		t.Fatalf("ws list reply: %q", r)
	}
	run("/new")
	if r := run("/ws use proj"); !strings.Contains(r, dir) {
		t.Fatalf("ws use reply: %q", r)
	}
	if state.chat("oc_1").Cwd != dir {
		t.Fatal("ws use did not switch cwd")
	}
	if r := run("/ws remove proj"); !strings.Contains(r, "proj") {
		t.Fatalf("ws remove reply: %q", r)
	}
}

func TestAgentHandleStatusAndHelp(t *testing.T) {
	runner := &fakeRunner{}
	replier := &fakeReplier{}
	state := newTestAgentState(t)
	state.setSession("oc_1", "sess-9")

	agentHandleMessage(context.Background(), runner, replier, state,
		incomingMessage{ChatID: "oc_1", Text: "/status"})
	if len(replier.replies) != 1 || !strings.Contains(replier.replies[0], "sess-9") {
		t.Fatalf("status reply: %v", replier.replies)
	}

	agentHandleMessage(context.Background(), runner, replier, state,
		incomingMessage{ChatID: "oc_1", Text: "/help"})
	if len(replier.replies) != 2 || !strings.Contains(replier.replies[1], "/cd") {
		t.Fatalf("help reply: %v", replier.replies)
	}
	if len(runner.calls) != 0 {
		t.Fatal("commands must not run agent")
	}
}

func TestAgentHandleUsesChatCwd(t *testing.T) {
	runner := &fakeRunner{res: agentResult{Text: "ok", SessionID: "s"}}
	replier := &fakeReplier{}
	state := newTestAgentState(t)
	dir := t.TempDir()
	if err := state.setCwd("oc_1", dir); err != nil {
		t.Fatal(err)
	}

	agentHandleMessage(context.Background(), runner, replier, state,
		incomingMessage{ChatID: "oc_1", Text: "hi"})

	if len(runner.cwds) != 1 || runner.cwds[0] != dir {
		t.Fatalf("runner cwds = %v, want %s", runner.cwds, dir)
	}
}

type fakeDownloader struct{ payload string }

func (f *fakeDownloader) DownloadMessageResource(_ context.Context, messageID, fileKey, resourceType string, w io.Writer) error {
	_, err := io.WriteString(w, f.payload)
	return err
}

func TestResolveMediaDownloadsAndRewrites(t *testing.T) {
	dl := &fakeDownloader{payload: "PNGDATA"}
	cwd := t.TempDir()
	msg := incomingMessage{ChatID: "oc_1", MessageID: "om_9", MediaType: "image", MediaKey: "img_1"}

	out, err := resolveMedia(context.Background(), dl, msg, cwd)
	if err != nil {
		t.Fatalf("resolveMedia: %v", err)
	}
	if !strings.Contains(out.Text, "media/") {
		t.Fatalf("text = %q", out.Text)
	}
	// The referenced file must exist with the payload.
	start := strings.Index(out.Text, cwd)
	if start < 0 {
		t.Fatalf("no path in text: %q", out.Text)
	}
	path := strings.Fields(out.Text[start:])[0]
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "PNGDATA" {
		t.Fatalf("file %s: %v %q", path, err, data)
	}
}
