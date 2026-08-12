package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
)

// UserIdentity is a redacted user profile returned by gateway APIs.
type UserIdentity struct {
	UserID    string `json:"user_id,omitempty"`
	Name      string `json:"name,omitempty"`
	OpenID    string `json:"open_id,omitempty"`
	UnionID   string `json:"union_id,omitempty"`
	Email     string `json:"email,omitempty"`
	TenantKey string `json:"tenant_key,omitempty"`
}

func normalizeBaseURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return defaultGatewayURL
	}
	return strings.TrimRight(trimmed, "/")
}

func pickString(data map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := data[key]; ok {
			s := strings.TrimSpace(fmt.Sprintf("%v", v))
			if s != "" && s != "<nil>" {
				return s
			}
		}
	}
	return ""
}

func randHex(bytesLen int) (string, error) {
	if bytesLen <= 0 {
		bytesLen = 16
	}
	buf := make([]byte, bytesLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func pickInt(data map[string]any, keys ...string) int {
	for _, key := range keys {
		if v, ok := data[key]; ok {
			n := intFromAny(v)
			if n != 0 {
				return n
			}
		}
	}
	return 0
}

func intFromAny(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case int32:
		return int(t)
	case int64:
		return int(t)
	case float64:
		return int(t)
	case json.Number:
		n, _ := t.Int64()
		return int(n)
	case string:
		t = strings.TrimSpace(t)
		if t == "" {
			return 0
		}
		var n int
		_, _ = fmt.Sscanf(t, "%d", &n)
		return n
	default:
		return 0
	}
}

func mustPathEscape(value string) string {
	escaped := url.PathEscape(value)
	if escaped == "" {
		return value
	}
	return escaped
}

func extractToken(input string) string {
	trimmed := strings.TrimSpace(strings.TrimRight(input, "/"))
	if trimmed == "" {
		return ""
	}
	u, err := url.Parse(trimmed)
	if err == nil && u.Host != "" {
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) > 0 {
			return parts[len(parts)-1]
		}
	}
	if idx := strings.LastIndex(trimmed, "/"); idx >= 0 {
		return trimmed[idx+1:]
	}
	return trimmed
}

// resolveTaskMembers resolves member strings (names or IDs) to Feishu task
// member objects. If a member looks like a user_id (numeric) or open_id
// (ou_ prefix), it's used directly. Otherwise, it's treated as a name and
// searched via the Feishu search API.
func resolveTaskMembers(ctx context.Context, client FeishuClient, raw []string) ([]map[string]any, error) {
	// Check if any member needs name resolution
	needsSearch := false
	for _, m := range raw {
		id := strings.TrimPrefix(strings.TrimPrefix(m, "assignee:"), "follower:")
		if !looksLikeUserID(strings.TrimSpace(id)) {
			needsSearch = true
			break
		}
	}
	if needsSearch {
		if err := requireScopes(ctx, client, contactSearchScopes); err != nil {
			return nil, err
		}
	}

	var members []map[string]any
	for _, m := range raw {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}

		role := "assignee"
		id := m
		if strings.HasPrefix(m, "assignee:") {
			id = strings.TrimPrefix(m, "assignee:")
		} else if strings.HasPrefix(m, "follower:") {
			role = "follower"
			id = strings.TrimPrefix(m, "follower:")
		}

		// If it looks like a user_id or open_id, use directly
		if looksLikeUserID(id) {
			members = append(members, map[string]any{
				"id": id, "type": "user", "role": role,
			})
			continue
		}

		// Otherwise search by name
		userID, err := searchUserByName(ctx, client, id)
		if err != nil {
			return nil, fmt.Errorf("resolve member %q: %w", id, err)
		}
		members = append(members, map[string]any{
			"id": userID, "type": "user", "role": role,
		})
	}
	return members, nil
}

// looksLikeUserID returns true if s looks like a numeric user_id or an open_id/union_id.
func looksLikeUserID(s string) bool {
	if strings.HasPrefix(s, "ou_") || strings.HasPrefix(s, "on_") {
		return true
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
}

// searchUserByName searches for a user by name and returns the first match's user_id.
func searchUserByName(ctx context.Context, client FeishuClient, name string) (string, error) {
	data, err := client.SearchUsers(ctx, name, 0)
	if err != nil {
		return "", fmt.Errorf("search users: %w", err)
	}

	m, _ := data.(map[string]any)

	// Try different response formats
	items, _ := m["items"].([]any)
	if len(items) == 0 {
		items, _ = m["users"].([]any)
	}
	if len(items) == 0 {
		items, _ = m["user_list"].([]any)
	}

	if len(items) == 0 {
		return "", fmt.Errorf("no user found matching %q", name)
	}

	// Return the first match
	first, _ := items[0].(map[string]any)
	userID := pickString(first, "user_id", "open_id")
	if userID == "" {
		return "", fmt.Errorf("user found but no user_id: %v", first)
	}

	userName := pickString(first, "name")
	fmt.Fprintf(os.Stderr, "=> Resolved %q → %s (%s)\n", name, userName, userID)
	return userID, nil
}

// resolveUserIDs resolves a list of names/IDs to user_ids.
func resolveUserIDs(ctx context.Context, client FeishuClient, raw []string) ([]string, error) {
	needsSearch := false
	for _, s := range raw {
		if !looksLikeUserID(strings.TrimSpace(s)) {
			needsSearch = true
			break
		}
	}
	if needsSearch {
		if err := requireScopes(ctx, client, contactSearchScopes); err != nil {
			return nil, err
		}
	}

	var result []string
	for _, s := range raw {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if looksLikeUserID(s) {
			result = append(result, s)
		} else {
			uid, err := searchUserByName(ctx, client, s)
			if err != nil {
				return nil, err
			}
			result = append(result, uid)
		}
	}
	return result, nil
}

func inferDocumentType(input, explicit string) (string, error) {
	switch explicit {
	case "", "auto":
		if strings.Contains(input, "/wiki/") {
			return "wiki", nil
		}
		return "document", nil
	case "wiki", "document":
		return explicit, nil
	default:
		return "", fmt.Errorf("invalid document type %q, expected auto|document|wiki", explicit)
	}
}
