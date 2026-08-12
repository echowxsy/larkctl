package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	feishuAuthBaseURL = "https://accounts.feishu.cn/open-apis"
	localCallbackPort = 19876
)

// localLogin performs OAuth authorization code flow using a temporary local HTTP server.
func localLogin(ctx context.Context, client *LocalClient, scopes string, openBrowser bool) error {
	addr := fmt.Sprintf("127.0.0.1:%d", localCallbackPort)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("start local server on %s: %w (is another larkctl login running?)", addr, err)
	}
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", localCallbackPort)

	state, err := randHex(16)
	if err != nil {
		listener.Close()
		return fmt.Errorf("generate state: %w", err)
	}

	// Build Feishu authorize URL
	authURL := feishuAuthBaseURL + "/authen/v1/authorize?" + url.Values{
		"app_id":        {client.appID},
		"redirect_uri":  {redirectURI},
		"state":         {state},
		"response_type": {"code"},
		"scope":         {scopes},
	}.Encode()

	fmt.Printf("Open this URL to authorize:\n  %s\n\n", authURL)
	if openBrowser {
		_ = tryOpenBrowser(authURL)
	}
	fmt.Println("Waiting for authorization callback...")

	// Channel to receive auth code
	type callbackResult struct {
		code string
		err  error
	}
	ch := make(chan callbackResult, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("state") != state {
			ch <- callbackResult{err: fmt.Errorf("state mismatch")}
			http.Error(w, "state mismatch", http.StatusBadRequest)
			return
		}
		code := q.Get("code")
		if code == "" {
			errMsg := q.Get("error")
			if errMsg == "" {
				errMsg = "no authorization code received"
			}
			ch <- callbackResult{err: fmt.Errorf("%s", errMsg)}
			http.Error(w, errMsg, http.StatusBadRequest)
			return
		}
		ch <- callbackResult{code: code}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html><html><body><h2>Authorization successful!</h2><p>You can close this window.</p><script>window.close()</script></body></html>`)
	})

	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(listener) }()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	// Wait for callback or context cancellation
	var result callbackResult
	select {
	case result = <-ch:
	case <-ctx.Done():
		return ctx.Err()
	}

	if result.err != nil {
		return result.err
	}

	// Exchange code for tokens
	tok, err := client.exchangeAuthCode(ctx, result.code, redirectURI)
	if err != nil {
		return fmt.Errorf("exchange auth code: %w", err)
	}

	expireAt := time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	client.SetTokens(tok.AccessToken, tok.RefreshToken, expireAt)

	if err := SaveLocalTokens(tok.AccessToken, tok.RefreshToken, expireAt); err != nil {
		return fmt.Errorf("save tokens: %w", err)
	}

	// Fetch and display user info
	user, err := client.fetchUser(ctx)
	if err != nil {
		fmt.Printf("Login successful (could not fetch user info: %v)\n", err)
		return nil
	}

	fmt.Printf("Login successful!\n")
	fmt.Printf("  Name:   %s\n", user.Name)
	fmt.Printf("  UserID: %s\n", user.UserID)
	if user.Email != "" {
		fmt.Printf("  Email:  %s\n", user.Email)
	}

	return nil
}

func buildDefaultScopes() string {
	var all []string
	all = append(all, strings.Fields(baseScopes)...)
	for _, name := range allScopeGroupNames() {
		g := scopeGroups[name]
		all = append(all, strings.Fields(g.Scopes)...)
	}
	// Deduplicate
	seen := map[string]bool{}
	var unique []string
	for _, s := range all {
		if !seen[s] {
			seen[s] = true
			unique = append(unique, s)
		}
	}
	return strings.Join(unique, " ")
}
