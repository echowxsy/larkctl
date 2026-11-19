package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScopeCacheRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), scopeCacheFileName)
	if cachedScopesCover(path, "tok", "a") {
		t.Fatal("empty cache must not cover anything")
	}
	storeCachedScopes(path, "tok", "a b c")
	if !cachedScopesCover(path, "tok", "a c") {
		t.Fatal("subset of cached scopes should be covered")
	}
	if cachedScopesCover(path, "tok", "a d") {
		t.Fatal("scope outside the cache must not be covered")
	}
	if cachedScopesCover(path, "other-token", "a") {
		t.Fatal("cache for another session must not apply")
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "tok\"") {
		t.Fatalf("token stored in clear: %s", data)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("perm = %v", fi.Mode().Perm())
	}
	storeCachedScopes(path, "tok", "") // empty (legacy) scopes are never cached
	if !cachedScopesCover(path, "tok", "a") {
		t.Fatal("empty scopes overwrote a good entry")
	}
}

func TestRequireScopesUsesCache(t *testing.T) {
	calls := 0
	var gotUA string
	mux := gatewayMux()
	mux.HandleFunc("/v1/session/upgrade", func(w http.ResponseWriter, r *http.Request) {
		calls++
		gotUA = r.Header.Get("User-Agent")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"upgrade_needed": false, "scopes": "calendar:calendar docx:document"},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	gc := NewGatewayClient(srv.URL)
	gc.SetSessionToken("test-token")
	gc.scopeCachePath = filepath.Join(t.TempDir(), scopeCacheFileName)

	for i := 0; i < 3; i++ {
		if err := requireScopes(context.Background(), gc, "calendar:calendar"); err != nil {
			t.Fatal(err)
		}
	}
	if err := requireScopes(context.Background(), gc, "docx:document"); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("upgrade called %d times, want 1", calls)
	}
	if !strings.HasPrefix(gotUA, "larkctl/") {
		t.Fatalf("User-Agent = %q", gotUA)
	}

	// A scope the session does not have still goes to the gateway.
	if err := requireScopes(context.Background(), gc, "calendar:calendar mail:mail"); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("upgrade called %d times, want 2", calls)
	}
}
