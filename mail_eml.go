package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"mime"
	"net/mail"
	"path/filepath"
	"strings"
	"time"
)

type MailAttachment struct {
	Filename string
	MIMEType string
	Data     []byte
}

type MailMessage struct {
	From        string
	To          []string
	Cc          []string
	Bcc         []string
	Subject     string
	BodyHTML    string
	BodyText    string
	Attachments []MailAttachment
	// In-Reply-To and References headers for reply/forward threading.
	InReplyTo  string
	References []string
}

// BuildEML builds an RFC 5822 / MIME message and returns it as base64url.
// Empty bodies fall back to a single empty text/plain part.
func (m *MailMessage) BuildEML() (string, error) {
	if len(m.To)+len(m.Cc)+len(m.Bcc) == 0 {
		return "", fmt.Errorf("no recipients (need at least one of --to/--cc/--bcc)")
	}
	for _, addrs := range [][]string{m.To, m.Cc, m.Bcc} {
		for _, a := range addrs {
			if _, err := mail.ParseAddress(a); err != nil {
				return "", fmt.Errorf("invalid address %q: %w", a, err)
			}
		}
	}

	var buf bytes.Buffer
	writeHeader := func(name, value string) {
		fmt.Fprintf(&buf, "%s: %s\r\n", name, value)
	}
	encodeAddr := func(a string) string {
		addr, err := mail.ParseAddress(a)
		if err != nil || addr.Name == "" {
			return a
		}
		return mime.BEncoding.Encode("UTF-8", addr.Name) + " <" + addr.Address + ">"
	}
	joinAddrs := func(addrs []string) string {
		out := make([]string, len(addrs))
		for i, a := range addrs {
			out[i] = encodeAddr(a)
		}
		return strings.Join(out, ", ")
	}

	if m.From != "" {
		writeHeader("From", encodeAddr(m.From))
	}
	if len(m.To) > 0 {
		writeHeader("To", joinAddrs(m.To))
	}
	if len(m.Cc) > 0 {
		writeHeader("Cc", joinAddrs(m.Cc))
	}
	if len(m.Bcc) > 0 {
		writeHeader("Bcc", joinAddrs(m.Bcc))
	}
	if m.Subject != "" {
		writeHeader("Subject", mime.BEncoding.Encode("UTF-8", m.Subject))
	}
	writeHeader("Date", time.Now().UTC().Format(time.RFC1123Z))
	writeHeader("MIME-Version", "1.0")
	if m.InReplyTo != "" {
		writeHeader("In-Reply-To", ensureAngle(m.InReplyTo))
	}
	if len(m.References) > 0 {
		refs := make([]string, len(m.References))
		for i, r := range m.References {
			refs[i] = ensureAngle(r)
		}
		writeHeader("References", strings.Join(refs, " "))
	}

	hasHTML := m.BodyHTML != ""
	hasText := m.BodyText != ""
	hasAttach := len(m.Attachments) > 0
	if !hasHTML && !hasText {
		hasText = true
	}

	writeBodyPart := func(b *bytes.Buffer, contentType, body string) {
		fmt.Fprintf(b, "Content-Type: %s; charset=\"UTF-8\"\r\n", contentType)
		b.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
		b.WriteString(wrap76(base64.StdEncoding.EncodeToString([]byte(body))))
		b.WriteString("\r\n")
	}

	bothBodies := hasHTML && hasText
	switch {
	case !hasAttach && !bothBodies:
		ct, body := "text/plain", m.BodyText
		if hasHTML {
			ct, body = "text/html", m.BodyHTML
		}
		writeBodyPart(&buf, ct, body)
	case !hasAttach && bothBodies:
		altBoundary := newBoundary()
		fmt.Fprintf(&buf, "Content-Type: multipart/alternative; boundary=\"%s\"\r\n\r\n", altBoundary)
		fmt.Fprintf(&buf, "--%s\r\n", altBoundary)
		writeBodyPart(&buf, "text/plain", m.BodyText)
		fmt.Fprintf(&buf, "--%s\r\n", altBoundary)
		writeBodyPart(&buf, "text/html", m.BodyHTML)
		fmt.Fprintf(&buf, "--%s--\r\n", altBoundary)
	default:
		mixedBoundary := newBoundary()
		fmt.Fprintf(&buf, "Content-Type: multipart/mixed; boundary=\"%s\"\r\n\r\n", mixedBoundary)
		fmt.Fprintf(&buf, "--%s\r\n", mixedBoundary)
		if bothBodies {
			altBoundary := newBoundary()
			fmt.Fprintf(&buf, "Content-Type: multipart/alternative; boundary=\"%s\"\r\n\r\n", altBoundary)
			fmt.Fprintf(&buf, "--%s\r\n", altBoundary)
			writeBodyPart(&buf, "text/plain", m.BodyText)
			fmt.Fprintf(&buf, "--%s\r\n", altBoundary)
			writeBodyPart(&buf, "text/html", m.BodyHTML)
			fmt.Fprintf(&buf, "--%s--\r\n", altBoundary)
		} else {
			ct, body := "text/plain", m.BodyText
			if hasHTML {
				ct, body = "text/html", m.BodyHTML
			}
			writeBodyPart(&buf, ct, body)
		}
		for _, att := range m.Attachments {
			mt := att.MIMEType
			if mt == "" {
				mt = mime.TypeByExtension(filepath.Ext(att.Filename))
			}
			if mt == "" {
				mt = "application/octet-stream"
			}
			encName := mime.BEncoding.Encode("UTF-8", att.Filename)
			fmt.Fprintf(&buf, "--%s\r\n", mixedBoundary)
			fmt.Fprintf(&buf, "Content-Type: %s; name=\"%s\"\r\n", mt, encName)
			fmt.Fprintf(&buf, "Content-Disposition: attachment; filename=\"%s\"\r\n", encName)
			buf.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
			buf.WriteString(wrap76(base64.StdEncoding.EncodeToString(att.Data)))
			buf.WriteString("\r\n")
		}
		fmt.Fprintf(&buf, "--%s--\r\n", mixedBoundary)
	}

	return base64.RawURLEncoding.EncodeToString(buf.Bytes()), nil
}

func wrap76(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i += 76 {
		end := i + 76
		if end > len(s) {
			end = len(s)
		}
		b.WriteString(s[i:end])
		b.WriteString("\r\n")
	}
	out := b.String()
	return strings.TrimSuffix(out, "\r\n")
}

func newBoundary() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return "larkctl_" + hex.EncodeToString(b[:])
}

func ensureAngle(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	if strings.HasPrefix(s, "<") && strings.HasSuffix(s, ">") {
		return s
	}
	return "<" + s + ">"
}
