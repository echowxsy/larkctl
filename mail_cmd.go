package main

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

const (
	mailScopeRead   = "mail:user_mailbox.message:readonly mail:user_mailbox.message.subject:read mail:user_mailbox.message.address:read mail:user_mailbox.message.body:read"
	mailScopeWrite  = "mail:user_mailbox.message:modify mail:user_mailbox.message:send"
	mailScopeFolder = "mail:user_mailbox.folder:read"
	mailScopeBase   = "mail:user_mailbox:readonly"
)

func newMailCmd() *cobra.Command {
	mailCmd := &cobra.Command{
		Use:   "mail",
		Short: "Feishu mail commands (list/read/send/reply/forward/...)",
	}

	mailCmd.AddCommand(
		newMailListCmd(),
		newMailReadCmd(),
		newMailThreadCmd(),
		newMailSearchCmd(),
		newMailSendCmd(),
		newMailReplyCmd(),
		newMailForwardCmd(),
		newMailTrashCmd(),
		newMailDraftDeleteCmd(),
		newMailStatusCmd(),
		newMailFoldersCmd(),
		newMailMailboxesCmd(),
		newMailSendAsCmd(),
		newMailProfileCmd(),
	)
	return mailCmd
}

func newMailDraftDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "draft-delete <draft-id>...",
		Short: "Hard-delete one or more drafts (irreversible; messages.trash is a no-op on drafts)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			if err := requireScopes(cmd.Context(), client, mailScopeWrite); err != nil {
				return err
			}
			results := make([]map[string]any, 0, len(args))
			for _, id := range args {
				_, err := client.DeleteMailDraft(cmd.Context(), id)
				entry := map[string]any{"draft_id": id}
				if err != nil {
					entry["error"] = err.Error()
				} else {
					entry["ok"] = true
				}
				results = append(results, entry)
			}
			printJSON(map[string]any{"results": results})
			return nil
		},
	}
}

func newMailListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List messages in a folder",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			if err := requireScopes(cmd.Context(), client, mailScopeRead); err != nil {
				return err
			}
			folder, _ := cmd.Flags().GetString("folder")
			label, _ := cmd.Flags().GetString("label")
			unread, _ := cmd.Flags().GetBool("unread")
			pageSize, _ := cmd.Flags().GetInt("limit")
			pageToken, _ := cmd.Flags().GetString("page-token")

			q := url.Values{"page_size": {strconv.Itoa(pageSize)}}
			if folder != "" {
				q.Set("folder_id", folder)
			}
			if label != "" {
				q.Set("label_id", label)
			}
			if unread {
				q.Set("only_unread", "true")
			}
			if pageToken != "" {
				q.Set("page_token", pageToken)
			}
			data, err := client.ListMailMessages(cmd.Context(), q)
			if err != nil {
				return err
			}
			printOutput(data)
			return nil
		},
	}
	cmd.Flags().String("folder", "INBOX", "folder id (INBOX, SENT, DRAFT, TRASH, SPAM, ARCHIVED, or custom)")
	cmd.Flags().String("label", "", "label id (e.g. FLAGGED, IMPORTANT)")
	cmd.Flags().Bool("unread", false, "only unread messages")
	cmd.Flags().Int("limit", 20, "page size (max 20)")
	cmd.Flags().String("page-token", "", "pagination token from a previous response")
	return cmd
}

func newMailReadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "read <message-id>",
		Short: "Read full content of a single message",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			if err := requireScopes(cmd.Context(), client, mailScopeRead); err != nil {
				return err
			}
			format := "full"
			if noHTML, _ := cmd.Flags().GetBool("no-html"); noHTML {
				format = "plain_text_full"
			}
			if metaOnly, _ := cmd.Flags().GetBool("metadata-only"); metaOnly {
				format = "metadata"
			}
			data, err := client.GetMailMessage(cmd.Context(), args[0], format)
			if err != nil {
				return err
			}
			printOutput(decodeMailBodies(data))
			return nil
		},
	}
	cmd.Flags().Bool("no-html", false, "return plain_text body only (skip HTML)")
	cmd.Flags().Bool("metadata-only", false, "return metadata only, no body content")
	return cmd
}

func newMailThreadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "thread <thread-id>",
		Short: "Read all messages in a thread",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			if err := requireScopes(cmd.Context(), client, mailScopeRead); err != nil {
				return err
			}
			format := "full"
			if noHTML, _ := cmd.Flags().GetBool("no-html"); noHTML {
				format = "plain_text_full"
			}
			includeSpamTrash, _ := cmd.Flags().GetBool("include-spam-trash")
			data, err := client.GetMailThread(cmd.Context(), args[0], format, includeSpamTrash)
			if err != nil {
				return err
			}
			printOutput(decodeMailBodies(data))
			return nil
		},
	}
	cmd.Flags().Bool("no-html", false, "return plain_text body only")
	cmd.Flags().Bool("include-spam-trash", false, "include messages from SPAM and TRASH")
	return cmd
}

func newMailSearchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Full-text search across mailbox",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			if err := requireScopes(cmd.Context(), client, mailScopeRead); err != nil {
				return err
			}
			pageSize, _ := cmd.Flags().GetInt("limit")
			pageToken, _ := cmd.Flags().GetString("page-token")
			q := url.Values{"page_size": {strconv.Itoa(pageSize)}}
			if pageToken != "" {
				q.Set("page_token", pageToken)
			}
			body := map[string]any{"query": args[0]}
			filter := map[string]any{}
			if v, _ := cmd.Flags().GetStringSlice("from"); len(v) > 0 {
				filter["from"] = v
			}
			if v, _ := cmd.Flags().GetStringSlice("to"); len(v) > 0 {
				filter["to"] = v
			}
			if v, _ := cmd.Flags().GetStringSlice("folder"); len(v) > 0 {
				filter["folder"] = v
			}
			if v, _ := cmd.Flags().GetBool("has-attachment"); v {
				filter["has_attachment"] = true
			}
			if v, _ := cmd.Flags().GetBool("unread"); v {
				filter["is_unread"] = true
			}
			if len(filter) > 0 {
				body["filter"] = filter
			}
			data, err := client.SearchMail(cmd.Context(), body, q)
			if err != nil {
				return err
			}
			printOutput(data)
			return nil
		},
	}
	cmd.Flags().StringSlice("from", nil, "filter by sender (repeatable)")
	cmd.Flags().StringSlice("to", nil, "filter by recipient (repeatable)")
	cmd.Flags().StringSlice("folder", nil, "limit to folder names (e.g. inbox, sent)")
	cmd.Flags().Bool("has-attachment", false, "only messages with attachments")
	cmd.Flags().Bool("unread", false, "only unread messages")
	cmd.Flags().Int("limit", 15, "page size (max 15)")
	cmd.Flags().String("page-token", "", "pagination token from a previous response")
	return cmd
}

func newMailSendCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "send",
		Short: "Compose and send a new email (default: send immediately; use --draft to save instead)",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			if err := requireScopes(cmd.Context(), client, mailScopeWrite); err != nil {
				return err
			}
			msg, err := mailMessageFromFlags(cmd)
			if err != nil {
				return err
			}
			return runMailSend(cmd, client, msg)
		},
	}
	addMailComposeFlags(cmd)
	cmd.Flags().StringSlice("to", nil, "recipient (repeatable)")
	cmd.Flags().StringSlice("cc", nil, "cc recipient (repeatable)")
	cmd.Flags().StringSlice("bcc", nil, "bcc recipient (repeatable)")
	cmd.Flags().String("subject", "", "subject line")
	return cmd
}

func newMailReplyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reply <message-id>",
		Short: "Reply to a message (default: send immediately; use --draft to save instead)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			if err := requireScopes(cmd.Context(), client, mailScopeRead+" "+mailScopeWrite); err != nil {
				return err
			}
			replyAll, _ := cmd.Flags().GetBool("all")
			origRaw, err := client.GetMailMessage(cmd.Context(), args[0], "metadata")
			if err != nil {
				return fmt.Errorf("fetch original message: %w", err)
			}
			orig, _ := origRaw.(map[string]any)
			origMsg, _ := orig["message"].(map[string]any)
			msg, err := mailMessageFromFlags(cmd)
			if err != nil {
				return err
			}
			subject, _ := origMsg["subject"].(string)
			if !strings.HasPrefix(strings.ToLower(subject), "re:") {
				subject = "Re: " + subject
			}
			msg.Subject = subject
			if len(msg.To) == 0 {
				if origFrom, ok := origMsg["head_from"].(map[string]any); ok {
					if addr, _ := origFrom["mail_address"].(string); addr != "" {
						msg.To = []string{addr}
					}
				}
			}
			if replyAll {
				if origTo, ok := origMsg["to"].([]any); ok {
					for _, a := range origTo {
						if am, ok := a.(map[string]any); ok {
							if addr, _ := am["mail_address"].(string); addr != "" {
								msg.To = appendUnique(msg.To, addr)
							}
						}
					}
				}
				if origCc, ok := origMsg["cc"].([]any); ok {
					for _, a := range origCc {
						if am, ok := a.(map[string]any); ok {
							if addr, _ := am["mail_address"].(string); addr != "" {
								msg.Cc = appendUnique(msg.Cc, addr)
							}
						}
					}
				}
			}
			if smtpMid, _ := origMsg["smtp_message_id"].(string); smtpMid != "" {
				msg.InReplyTo = smtpMid
				msg.References = []string{smtpMid}
			}
			return runMailSend(cmd, client, msg)
		},
	}
	addMailComposeFlags(cmd)
	cmd.Flags().Bool("all", false, "reply to all (include original To and CC)")
	cmd.Flags().StringSlice("to", nil, "override To (default: original sender)")
	cmd.Flags().StringSlice("cc", nil, "extra cc (additive)")
	cmd.Flags().StringSlice("bcc", nil, "bcc (private)")
	return cmd
}

func newMailForwardCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "forward <message-id>",
		Short: "Forward a message (default: send immediately; use --draft to save instead)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			if err := requireScopes(cmd.Context(), client, mailScopeRead+" "+mailScopeWrite); err != nil {
				return err
			}
			origRaw, err := client.GetMailMessage(cmd.Context(), args[0], "metadata")
			if err != nil {
				return fmt.Errorf("fetch original message: %w", err)
			}
			orig, _ := origRaw.(map[string]any)
			origMsg, _ := orig["message"].(map[string]any)
			msg, err := mailMessageFromFlags(cmd)
			if err != nil {
				return err
			}
			if len(msg.To) == 0 {
				return fmt.Errorf("--to is required for forward")
			}
			subject, _ := origMsg["subject"].(string)
			if !strings.HasPrefix(strings.ToLower(subject), "fw:") && !strings.HasPrefix(strings.ToLower(subject), "fwd:") {
				subject = "Fw: " + subject
			}
			msg.Subject = subject
			if smtpMid, _ := origMsg["smtp_message_id"].(string); smtpMid != "" {
				msg.References = []string{smtpMid}
			}
			return runMailSend(cmd, client, msg)
		},
	}
	addMailComposeFlags(cmd)
	cmd.Flags().StringSlice("to", nil, "recipient (required)")
	cmd.Flags().StringSlice("cc", nil, "cc recipient")
	cmd.Flags().StringSlice("bcc", nil, "bcc recipient")
	return cmd
}

func newMailTrashCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trash <message-id>...",
		Short: "Move one or more messages to trash",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			if err := requireScopes(cmd.Context(), client, mailScopeWrite); err != nil {
				return err
			}
			results := make([]map[string]any, 0, len(args))
			for _, id := range args {
				_, err := client.TrashMailMessage(cmd.Context(), id)
				entry := map[string]any{"message_id": id}
				if err != nil {
					entry["error"] = err.Error()
				} else {
					entry["ok"] = true
				}
				results = append(results, entry)
			}
			printJSON(map[string]any{"results": results})
			return nil
		},
	}
	return cmd
}

func newMailStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status <message-id>",
		Short: "Query delivery status for a sent message",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			if err := requireScopes(cmd.Context(), client, mailScopeRead); err != nil {
				return err
			}
			data, err := client.GetMailSendStatus(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}
	return cmd
}

func newMailFoldersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "folders",
		Short: "List mailbox folders",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			if err := requireScopes(cmd.Context(), client, mailScopeFolder); err != nil {
				return err
			}
			folderType, _ := cmd.Flags().GetInt("type")
			data, err := client.ListMailFolders(cmd.Context(), folderType)
			if err != nil {
				return err
			}
			printOutput(data)
			return nil
		},
	}
	cmd.Flags().Int("type", 0, "folder type: 1=system, 2=user, 0=all")
	return cmd
}

func newMailMailboxesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mailboxes",
		Short: "List accessible mailboxes (primary + public + aliases)",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			if err := requireScopes(cmd.Context(), client, mailScopeBase); err != nil {
				return err
			}
			data, err := client.ListAccessibleMailboxes(cmd.Context())
			if err != nil {
				return err
			}
			printOutput(data)
			return nil
		},
	}
}

func newMailSendAsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "send-as",
		Short: "List sendable addresses (primary + aliases + mail groups)",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			if err := requireScopes(cmd.Context(), client, mailScopeBase); err != nil {
				return err
			}
			data, err := client.GetMailSendAs(cmd.Context())
			if err != nil {
				return err
			}
			printOutput(data)
			return nil
		},
	}
}

func newMailProfileCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "profile",
		Short: "Show current mailbox profile (primary email)",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			if err := requireScopes(cmd.Context(), client, mailScopeBase); err != nil {
				return err
			}
			data, err := client.GetMailProfile(cmd.Context())
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}
}

func addMailComposeFlags(cmd *cobra.Command) {
	cmd.Flags().String("body", "", "body content (HTML by default; use --plain for plain text)")
	cmd.Flags().String("body-file", "", "read body from a file (use - for stdin)")
	cmd.Flags().Bool("plain", false, "treat body as plain text (default: HTML)")
	cmd.Flags().StringSlice("attach", nil, "attach a file (repeatable)")
	cmd.Flags().String("from", "", "From: address (alias / public mailbox)")
	cmd.Flags().Bool("draft", false, "save as draft instead of sending immediately")
	cmd.Flags().String("send-time", "", "schedule send at unix timestamp (seconds); requires --draft=false")
}

func mailMessageFromFlags(cmd *cobra.Command) (*MailMessage, error) {
	to, _ := cmd.Flags().GetStringSlice("to")
	cc, _ := cmd.Flags().GetStringSlice("cc")
	bcc, _ := cmd.Flags().GetStringSlice("bcc")
	subject, _ := cmd.Flags().GetString("subject")
	body, _ := cmd.Flags().GetString("body")
	bodyFile, _ := cmd.Flags().GetString("body-file")
	plain, _ := cmd.Flags().GetBool("plain")
	attach, _ := cmd.Flags().GetStringSlice("attach")
	from, _ := cmd.Flags().GetString("from")

	if bodyFile != "" {
		raw, err := readBodyFile(bodyFile)
		if err != nil {
			return nil, fmt.Errorf("read body-file: %w", err)
		}
		body = raw
	}

	msg := &MailMessage{
		To:      to,
		Cc:      cc,
		Bcc:     bcc,
		Subject: subject,
		From:    from,
	}
	if plain {
		msg.BodyText = body
	} else {
		msg.BodyHTML = body
	}
	for _, p := range attach {
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("read attach %q: %w", p, err)
		}
		msg.Attachments = append(msg.Attachments, MailAttachment{
			Filename: filepath.Base(p),
			Data:     data,
		})
	}
	return msg, nil
}

func runMailSend(cmd *cobra.Command, client FeishuClient, msg *MailMessage) error {
	if msg.From == "" {
		profile, err := client.GetMailProfile(cmd.Context())
		if err != nil {
			return fmt.Errorf("--from is required; failed to auto-fetch primary mailbox: %w", err)
		}
		if pm, ok := profile.(map[string]any); ok {
			msg.From, _ = pm["primary_email_address"].(string)
		}
		if msg.From == "" {
			return fmt.Errorf("--from is required (could not derive from profile)")
		}
	}
	raw, err := msg.BuildEML()
	if err != nil {
		return err
	}
	draft, err := client.CreateMailDraft(cmd.Context(), raw)
	if err != nil {
		return fmt.Errorf("create draft: %w", err)
	}
	draftMap, _ := draft.(map[string]any)
	draftInner, _ := draftMap["draft"].(map[string]any)
	draftID, _ := draftInner["id"].(string)
	if draftID == "" {
		return fmt.Errorf("create draft returned no id; response=%v", draft)
	}

	saveDraftOnly, _ := cmd.Flags().GetBool("draft")
	if saveDraftOnly {
		printJSON(map[string]any{"draft_id": draftID, "draft": draftInner})
		return nil
	}
	sendTime, _ := cmd.Flags().GetString("send-time")
	sendRes, err := client.SendMailDraft(cmd.Context(), draftID, sendTime)
	if err != nil {
		return fmt.Errorf("send draft: %w", err)
	}
	printJSON(map[string]any{"draft_id": draftID, "send_result": sendRes})
	return nil
}

func readBodyFile(p string) (string, error) {
	if p == "-" {
		data, err := readAllStdin()
		return string(data), err
	}
	data, err := os.ReadFile(p)
	return string(data), err
}

func readAllStdin() ([]byte, error) {
	return io.ReadAll(os.Stdin)
}

func appendUnique(xs []string, v string) []string {
	for _, x := range xs {
		if x == v {
			return xs
		}
	}
	return append(xs, v)
}

// decodeMailBodies recursively walks a mail response and decodes base64url fields
// like body_html, body_plain_text, body_preview, body_calendar so the output is human-readable.
func decodeMailBodies(data any) any {
	switch v := data.(type) {
	case map[string]any:
		for k, val := range v {
			if s, ok := val.(string); ok {
				if k == "body_html" || k == "body_plain_text" || k == "body_preview" || k == "body_calendar" {
					if decoded, err := decodeBase64URL(s); err == nil {
						v[k] = decoded
						continue
					}
				}
			}
			v[k] = decodeMailBodies(val)
		}
		return v
	case []any:
		for i, item := range v {
			v[i] = decodeMailBodies(item)
		}
		return v
	default:
		return v
	}
}

func decodeBase64URL(s string) (string, error) {
	s = strings.TrimRight(s, "=")
	if dec, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return string(dec), nil
	}
	if dec, err := base64.RawStdEncoding.DecodeString(s); err == nil {
		return string(dec), nil
	}
	return "", fmt.Errorf("not base64url")
}
