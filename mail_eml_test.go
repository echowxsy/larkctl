package main

import (
	"encoding/base64"
	"net/mail"
	"strings"
	"testing"
)

func parseBack(t *testing.T, eml64 string) *mail.Message {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(eml64)
	if err != nil {
		t.Fatalf("base64url decode: %v", err)
	}
	msg, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatalf("parse RFC 5322: %v\nraw:\n%s", err, raw)
	}
	return msg
}

func TestBuildEML_SimpleHTML(t *testing.T) {
	m := &MailMessage{
		From:     "Alice <alice@example.com>",
		To:       []string{"bob@example.com"},
		Subject:  "测试主题",
		BodyHTML: "<p>hello world</p>",
	}
	out, err := m.BuildEML()
	if err != nil {
		t.Fatalf("BuildEML: %v", err)
	}
	parsed := parseBack(t, out)
	if got := parsed.Header.Get("To"); got != "bob@example.com" {
		t.Errorf("To = %q", got)
	}
	if got := parsed.Header.Get("Subject"); !strings.Contains(got, "=?UTF-8?") {
		t.Errorf("Subject should be RFC 2047 encoded, got %q", got)
	}
	if !strings.Contains(parsed.Header.Get("Content-Type"), "text/html") {
		t.Errorf("Content-Type = %q", parsed.Header.Get("Content-Type"))
	}
}

func TestBuildEML_RequiresRecipient(t *testing.T) {
	m := &MailMessage{Subject: "x", BodyText: "y"}
	if _, err := m.BuildEML(); err == nil {
		t.Fatal("expected error when no recipients")
	}
}

func TestBuildEML_RejectsInvalidAddress(t *testing.T) {
	m := &MailMessage{To: []string{"not-an-email"}, Subject: "x", BodyText: "y"}
	if _, err := m.BuildEML(); err == nil {
		t.Fatal("expected error for invalid address")
	}
}

func TestBuildEML_WithAttachment(t *testing.T) {
	m := &MailMessage{
		To:       []string{"bob@example.com"},
		Subject:  "with attach",
		BodyText: "see attached",
		Attachments: []MailAttachment{
			{Filename: "test.txt", Data: []byte("hello attachment content")},
		},
	}
	out, err := m.BuildEML()
	if err != nil {
		t.Fatalf("BuildEML: %v", err)
	}
	parsed := parseBack(t, out)
	ct := parsed.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "multipart/mixed") {
		t.Errorf("Content-Type should be multipart/mixed, got %q", ct)
	}
}

func TestBuildEML_ReplyHeaders(t *testing.T) {
	m := &MailMessage{
		To:         []string{"bob@example.com"},
		Subject:    "Re: hi",
		BodyText:   "reply body",
		InReplyTo:  "abc@feishu",
		References: []string{"abc@feishu", "def@feishu"},
	}
	out, err := m.BuildEML()
	if err != nil {
		t.Fatalf("BuildEML: %v", err)
	}
	parsed := parseBack(t, out)
	if got := parsed.Header.Get("In-Reply-To"); got != "<abc@feishu>" {
		t.Errorf("In-Reply-To = %q", got)
	}
	if got := parsed.Header.Get("References"); got != "<abc@feishu> <def@feishu>" {
		t.Errorf("References = %q", got)
	}
}

func TestBuildEML_BothBodies(t *testing.T) {
	m := &MailMessage{
		To:       []string{"bob@example.com"},
		Subject:  "both",
		BodyHTML: "<p>html</p>",
		BodyText: "plain",
	}
	out, err := m.BuildEML()
	if err != nil {
		t.Fatalf("BuildEML: %v", err)
	}
	parsed := parseBack(t, out)
	if !strings.HasPrefix(parsed.Header.Get("Content-Type"), "multipart/alternative") {
		t.Errorf("Content-Type = %q", parsed.Header.Get("Content-Type"))
	}
}
