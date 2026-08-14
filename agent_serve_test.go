package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseClaudeStream(t *testing.T) {
	input := `{"type":"system","subtype":"init","session_id":"sess-1","cwd":"/tmp"}
{"type":"assistant","message":{"content":[{"type":"text","text":"thinking..."}]}}
{"type":"result","subtype":"success","result":"the answer is 42","session_id":"sess-1","is_error":false,"total_cost_usd":0.01}
`
	res, err := parseClaudeStream(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if res.Text != "the answer is 42" || res.SessionID != "sess-1" || res.IsError {
		t.Fatalf("res = %+v", res)
	}
}

func TestParseClaudeStreamErrorResult(t *testing.T) {
	input := `{"type":"system","subtype":"init","session_id":"s2"}
{"type":"result","subtype":"error_during_execution","result":"boom","session_id":"s2","is_error":true}
`
	res, err := parseClaudeStream(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !res.IsError || res.SessionID != "s2" {
		t.Fatalf("res = %+v", res)
	}
}

func TestParseClaudeStreamNoResult(t *testing.T) {
	res, err := parseClaudeStream(strings.NewReader(`{"type":"system","subtype":"init","session_id":"s3"}` + "\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if res.Text != "" || res.SessionID != "s3" {
		t.Fatalf("res = %+v", res)
	}
}

const sampleEnvelope = `{"event":{"sender":{"sender_id":{"user_id":"12345","open_id":"ou_abc"},"sender_type":"user"},` +
	`"message":{"message_id":"om_1","create_time":"1786617572173","chat_id":"oc_1","chat_type":"p2p",` +
	`"message_type":"text","content":"{\"text\":\"帮我看看这个项目\"}"}},` +
	`"header":{"event_id":"ev1","event_type":"im.message.receive_v1"},"schema":"2.0"}`

func TestParseIncomingMessage(t *testing.T) {
	msg, ok := parseIncomingMessage([]byte(sampleEnvelope))
	if !ok {
		t.Fatal("not recognized as text message")
	}
	if msg.ChatID != "oc_1" || msg.MessageID != "om_1" || msg.ChatType != "p2p" {
		t.Fatalf("msg = %+v", msg)
	}
	if msg.Text != "帮我看看这个项目" {
		t.Fatalf("text = %q", msg.Text)
	}
}

func TestParseIncomingMessageUnsupportedType(t *testing.T) {
	env := strings.Replace(sampleEnvelope, `"message_type":"text"`, `"message_type":"audio"`, 1)
	if _, ok := parseIncomingMessage([]byte(env)); ok {
		t.Fatal("audio message should be skipped")
	}
}

func TestBotReplyClient(t *testing.T) {
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/bot/reply" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		fmt.Fprint(w, `{"code":"ok","data":{"message_id":"om_bot"}}`)
	}))
	defer srv.Close()

	c := NewGatewayClient(srv.URL)
	c.SetSessionToken("tok")
	if err := c.BotReply(context.Background(), "oc_9", "hi", "om_orig"); err != nil {
		t.Fatalf("BotReply: %v", err)
	}
	if got["chat_id"] != "oc_9" || got["text"] != "hi" || got["reply_to_message_id"] != "om_orig" {
		t.Fatalf("request = %v", got)
	}
}

// fakes for orchestration

type fakeRunner struct {
	calls []struct{ prompt, session string }
	cwds  []string
	res   agentResult
	err   error
}

func (f *fakeRunner) Run(_ context.Context, req agentRunReq) (agentResult, error) {
	f.calls = append(f.calls, struct{ prompt, session string }{req.Prompt, req.SessionID})
	f.cwds = append(f.cwds, req.Cwd)
	return f.res, f.err
}

type fakeReplier struct{ replies []string }

func (f *fakeReplier) BotReply(_ context.Context, chatID, text, replyTo string) error {
	f.replies = append(f.replies, chatID+"|"+text)
	return nil
}

func TestAgentHandleMessageRunsAndReplies(t *testing.T) {
	runner := &fakeRunner{res: agentResult{Text: "done!", SessionID: "sess-new"}}
	replier := &fakeReplier{}
	state := newTestAgentState(t)

	agentHandleMessage(context.Background(), runner, replier, state,
		incomingMessage{ChatID: "oc_1", MessageID: "om_1", Text: "do the thing"})

	if len(runner.calls) != 1 || runner.calls[0].prompt != "do the thing" || runner.calls[0].session != "" {
		t.Fatalf("runner calls = %+v", runner.calls)
	}
	if state.chat("oc_1").Session != "sess-new" {
		t.Fatalf("session not stored: %q", state.chat("oc_1").Session)
	}
	if len(replier.replies) != 1 || replier.replies[0] != "oc_1|done!" {
		t.Fatalf("replies = %v", replier.replies)
	}
}

func TestAgentHandleMessageResumesSession(t *testing.T) {
	runner := &fakeRunner{res: agentResult{Text: "again", SessionID: "sess-1"}}
	replier := &fakeReplier{}
	state := newTestAgentState(t)
	state.setSession("oc_1", "sess-1")

	agentHandleMessage(context.Background(), runner, replier, state,
		incomingMessage{ChatID: "oc_1", Text: "follow up"})

	if runner.calls[0].session != "sess-1" {
		t.Fatalf("expected resume with sess-1, got %q", runner.calls[0].session)
	}
}

func TestAgentHandleMessageNewCommand(t *testing.T) {
	runner := &fakeRunner{}
	replier := &fakeReplier{}
	state := newTestAgentState(t)
	state.setSession("oc_1", "sess-old")

	agentHandleMessage(context.Background(), runner, replier, state,
		incomingMessage{ChatID: "oc_1", Text: "/new"})

	if len(runner.calls) != 0 {
		t.Fatal("/new must not invoke the agent")
	}
	if state.chat("oc_1").Session != "" {
		t.Fatal("session not cleared")
	}
	if len(replier.replies) != 1 {
		t.Fatalf("replies = %v", replier.replies)
	}
}

func TestAgentHandleMessageRunError(t *testing.T) {
	runner := &fakeRunner{err: fmt.Errorf("claude exploded")}
	replier := &fakeReplier{}
	state := newTestAgentState(t)

	agentHandleMessage(context.Background(), runner, replier, state,
		incomingMessage{ChatID: "oc_1", Text: "hi"})

	if len(replier.replies) != 1 || !strings.Contains(replier.replies[0], "claude exploded") {
		t.Fatalf("error not reported to chat: %v", replier.replies)
	}
}

func TestAgentTruncateReply(t *testing.T) {
	long := strings.Repeat("字", agentReplyMaxRunes+100)
	got := truncateReply(long)
	if len([]rune(got)) > agentReplyMaxRunes+50 {
		t.Fatalf("not truncated: %d runes", len([]rune(got)))
	}
	if !strings.Contains(got, "truncated") {
		t.Fatal("missing truncation marker")
	}
}
