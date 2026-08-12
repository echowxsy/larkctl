package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionTokenStoredInLocalConfig(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv(sessionEnvKey, "")

	baseURL := "http://127.0.0.1:8787"
	if err := SaveSessionToken(baseURL, "session-a"); err != nil {
		t.Fatalf("SaveSessionToken() error = %v", err)
	}

	got, err := LoadSessionToken(baseURL)
	if err != nil {
		t.Fatalf("LoadSessionToken() error = %v", err)
	}
	if got != "session-a" {
		t.Fatalf("session token = %q, want %q", got, "session-a")
	}

	configPath := filepath.Join(tmp, ".lark", "config.json")
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config file: %v", err)
	}

	var cfg localConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("decode config file: %v", err)
	}
	if cfg.GatewaySessions["session-token:127.0.0.1:8787"] != "session-a" {
		t.Fatalf("gateway session mismatch: %+v", cfg.GatewaySessions)
	}
}

func TestDeleteSessionToken(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv(sessionEnvKey, "")

	baseURL := "http://127.0.0.1:8787"
	if err := SaveSessionToken(baseURL, "session-a"); err != nil {
		t.Fatalf("SaveSessionToken() error = %v", err)
	}
	if err := DeleteSessionToken(baseURL); err != nil {
		t.Fatalf("DeleteSessionToken() error = %v", err)
	}
	if _, err := LoadSessionToken(baseURL); err == nil {
		t.Fatalf("LoadSessionToken() expected error after delete")
	}
}

func TestGatewayURLStoredInLocalConfig(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	const rawGatewayURL = "https://larkctl.example.com/"
	if err := SaveGatewayURL(rawGatewayURL); err != nil {
		t.Fatalf("SaveGatewayURL() error = %v", err)
	}

	got, err := LoadGatewayURL()
	if err != nil {
		t.Fatalf("LoadGatewayURL() error = %v", err)
	}
	if got != "https://larkctl.example.com" {
		t.Fatalf("gateway url = %q, want %q", got, "https://larkctl.example.com")
	}

	configPath := filepath.Join(tmp, ".lark", "config.json")
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config file: %v", err)
	}

	var cfg localConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("decode config file: %v", err)
	}
	if cfg.GatewayURL != "https://larkctl.example.com" {
		t.Fatalf("gateway_url = %q, want %q", cfg.GatewayURL, "https://larkctl.example.com")
	}
}

func TestSaveAndLoadLocalApp(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("FEISHU_APP_ID", "")
	t.Setenv("FEISHU_APP_SECRET", "")

	if err := SaveLocalApp("app-id-123", "secret-456"); err != nil {
		t.Fatalf("SaveLocalApp() error = %v", err)
	}

	appID, appSecret, err := LoadLocalApp()
	if err != nil {
		t.Fatalf("LoadLocalApp() error = %v", err)
	}
	if appID != "app-id-123" {
		t.Fatalf("appID = %q, want %q", appID, "app-id-123")
	}
	if appSecret != "secret-456" {
		t.Fatalf("appSecret = %q, want %q", appSecret, "secret-456")
	}
}

func TestLoadLocalAppFromEnv(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("FEISHU_APP_ID", "env-app-id")
	t.Setenv("FEISHU_APP_SECRET", "env-secret")

	appID, appSecret, err := LoadLocalApp()
	if err != nil {
		t.Fatalf("LoadLocalApp() error = %v", err)
	}
	if appID != "env-app-id" {
		t.Fatalf("appID = %q, want %q", appID, "env-app-id")
	}
	if appSecret != "env-secret" {
		t.Fatalf("appSecret = %q, want %q", appSecret, "env-secret")
	}
}

func TestLoadLocalAppErrorWhenNotConfigured(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("FEISHU_APP_ID", "")
	t.Setenv("FEISHU_APP_SECRET", "")

	_, _, err := LoadLocalApp()
	if err == nil {
		t.Fatalf("LoadLocalApp() expected error when not configured")
	}
}

func TestSaveAndLoadLocalTokens(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	expireAt := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	if err := SaveLocalTokens("access-tok", "refresh-tok", expireAt); err != nil {
		t.Fatalf("SaveLocalTokens() error = %v", err)
	}

	access, refresh, expire, err := LoadLocalTokens()
	if err != nil {
		t.Fatalf("LoadLocalTokens() error = %v", err)
	}
	if access != "access-tok" {
		t.Fatalf("access = %q, want %q", access, "access-tok")
	}
	if refresh != "refresh-tok" {
		t.Fatalf("refresh = %q, want %q", refresh, "refresh-tok")
	}
	if !expire.Equal(expireAt) {
		t.Fatalf("expireAt = %v, want %v", expire, expireAt)
	}
}

func TestLoadLocalTokensErrorWhenNotLoggedIn(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	_, _, _, err := LoadLocalTokens()
	if err == nil {
		t.Fatalf("LoadLocalTokens() expected error when not logged in")
	}
}

func TestDeleteLocalTokens(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	expireAt := time.Now().Add(time.Hour)
	if err := SaveLocalTokens("access", "refresh", expireAt); err != nil {
		t.Fatalf("SaveLocalTokens() error = %v", err)
	}
	if err := DeleteLocalTokens(); err != nil {
		t.Fatalf("DeleteLocalTokens() error = %v", err)
	}
	_, _, _, err := LoadLocalTokens()
	if err == nil {
		t.Fatalf("LoadLocalTokens() expected error after delete")
	}
}

func TestIsLocalMode(t *testing.T) {
	t.Run("env var set to local", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("HOME", tmp)
		t.Setenv("LARKCTL_MODE", "local")
		if !IsLocalMode() {
			t.Fatalf("IsLocalMode() = false, want true")
		}
	})

	t.Run("app credentials configured", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("HOME", tmp)
		t.Setenv("LARKCTL_MODE", "")
		if err := SaveLocalApp("app-id", "secret"); err != nil {
			t.Fatalf("SaveLocalApp() error = %v", err)
		}
		if !IsLocalMode() {
			t.Fatalf("IsLocalMode() = false, want true")
		}
	})

	t.Run("no credentials no env", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("HOME", tmp)
		t.Setenv("LARKCTL_MODE", "")
		t.Setenv("FEISHU_APP_ID", "")
		t.Setenv("FEISHU_APP_SECRET", "")
		if IsLocalMode() {
			t.Fatalf("IsLocalMode() = true, want false")
		}
	})
}

func TestSessionAccount(t *testing.T) {
	cases := []struct {
		name    string
		baseURL string
		want    string
	}{
		{name: "valid URL", baseURL: "http://127.0.0.1:8787", want: "session-token:127.0.0.1:8787"},
		{name: "https URL", baseURL: "https://larkctl.example.com", want: "session-token:larkctl.example.com"},
		{name: "invalid URL", baseURL: "://bad", want: "session-token:default"},
		{name: "empty URL", baseURL: "", want: "session-token:default"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sessionAccount(tc.baseURL)
			if got != tc.want {
				t.Fatalf("sessionAccount(%q) = %q, want %q", tc.baseURL, got, tc.want)
			}
		})
	}
}

func TestSaveSessionTokenRejectsEmpty(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	if err := SaveSessionToken("http://localhost", ""); err == nil {
		t.Fatalf("SaveSessionToken() expected error for empty token")
	}
	if err := SaveSessionToken("http://localhost", "   "); err == nil {
		t.Fatalf("SaveSessionToken() expected error for whitespace-only token")
	}
}

func TestLoadSessionTokenFromEnv(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv(sessionEnvKey, "env-token-123")

	got, err := LoadSessionToken("http://localhost")
	if err != nil {
		t.Fatalf("LoadSessionToken() error = %v", err)
	}
	if got != "env-token-123" {
		t.Fatalf("token = %q, want %q", got, "env-token-123")
	}
}

func TestDeleteSessionTokenNoOp(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	// Delete on empty config should not error
	if err := DeleteSessionToken("http://localhost"); err != nil {
		t.Fatalf("DeleteSessionToken() error = %v", err)
	}
}
