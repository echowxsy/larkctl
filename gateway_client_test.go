package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestClientAutoRefreshOnSessionExpired(t *testing.T) {
	t.Parallel()

	var infoCalls int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/docs/info":
			callNum := atomic.AddInt32(&infoCalls, 1)
			auth := r.Header.Get("Authorization")
			if callNum == 1 {
				// First call: return session_expired to trigger refresh
				if auth != "Bearer my-client-token" {
					t.Fatalf("first call auth = %q", auth)
				}
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"code":    "session_expired",
					"message": "expired",
				})
				return
			}
			// Second call after refresh: same client_token (gateway rotated session internally)
			if auth != "Bearer my-client-token" {
				t.Fatalf("second call auth = %q, want same client token", auth)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":    "ok",
				"message": "ok",
				"data":    map[string]any{"ok": true},
			})
		case "/v1/session/refresh":
			auth := r.Header.Get("Authorization")
			if auth != "Bearer my-client-token" {
				t.Fatalf("refresh auth = %q", auth)
			}
			// Gateway rotates session internally, client token stays the same
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":    "ok",
				"message": "ok",
				"data":    map[string]any{"refreshed": true},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := NewGatewayClient(srv.URL)
	client.SetSessionToken("my-client-token")

	_, err := client.GetDocumentInfo(context.Background(), "doc123", "document")
	if err != nil {
		t.Fatalf("GetDocumentInfo() error = %v", err)
	}

	// Client token should remain unchanged
	if got := client.session(); got != "my-client-token" {
		t.Fatalf("client token = %q, want my-client-token (should not change)", got)
	}
}
