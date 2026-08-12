package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

const sessionEnvKey = "LARKCTL_SESSION_TOKEN"

type localConfig struct {
	GatewaySessions map[string]string `json:"gateway_sessions,omitempty"`
	GatewayURL      string            `json:"gateway_url,omitempty"`

	// Local mode fields
	AppID          string `json:"app_id,omitempty"`
	AppSecret      string `json:"app_secret,omitempty"`
	AccessToken    string `json:"access_token,omitempty"`
	RefreshToken   string `json:"refresh_token,omitempty"`
	AccessExpireAt string `json:"access_expire_at,omitempty"` // RFC3339
}

func sessionAccount(baseURL string) string {
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" {
		return "session-token:default"
	}
	return "session-token:" + u.Host
}

func SaveSessionToken(baseURL, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return fmt.Errorf("empty session token")
	}

	cfg, err := loadLocalConfig()
	if err != nil {
		return err
	}
	if cfg.GatewaySessions == nil {
		cfg.GatewaySessions = map[string]string{}
	}
	cfg.GatewaySessions[sessionAccount(baseURL)] = token
	return saveLocalConfig(cfg)
}

func LoadSessionToken(baseURL string) (string, error) {
	if token := strings.TrimSpace(os.Getenv(sessionEnvKey)); token != "" {
		return token, nil
	}

	cfg, err := loadLocalConfig()
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(cfg.GatewaySessions[sessionAccount(baseURL)])
	if token == "" {
		return "", fmt.Errorf("session token not found, run `larkctl login`")
	}
	return token, nil
}

func DeleteSessionToken(baseURL string) error {
	cfg, err := loadLocalConfig()
	if err != nil {
		return err
	}
	if len(cfg.GatewaySessions) == 0 {
		return nil
	}
	delete(cfg.GatewaySessions, sessionAccount(baseURL))
	return saveLocalConfig(cfg)
}

func SaveGatewayURL(baseURL string) error {
	cfg, err := loadLocalConfig()
	if err != nil {
		return err
	}
	cfg.GatewayURL = normalizeBaseURL(baseURL)
	return saveLocalConfig(cfg)
}

func LoadGatewayURL() (string, error) {
	cfg, err := loadLocalConfig()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(cfg.GatewayURL), nil
}

func loadLocalConfig() (localConfig, error) {
	path, err := resolveLarkConfigPath()
	if err != nil {
		return localConfig{}, err
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return localConfig{}, nil
		}
		return localConfig{}, fmt.Errorf("read local config %s: %w", path, err)
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return localConfig{}, nil
	}

	var cfg localConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return localConfig{}, fmt.Errorf("decode local config %s: %w", path, err)
	}
	if cfg.GatewaySessions == nil {
		cfg.GatewaySessions = map[string]string{}
	}
	return cfg, nil
}

func saveLocalConfig(cfg localConfig) error {
	path, err := resolveLarkConfigPath()
	if err != nil {
		return err
	}
	if cfg.GatewaySessions == nil {
		cfg.GatewaySessions = map[string]string{}
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal local config %s: %w", path, err)
	}
	data = append(data, '\n')
	if err := writeFileAtomic(path, data, 0o600); err != nil {
		return err
	}
	return nil
}

func SaveLocalApp(appID, appSecret string) error {
	cfg, err := loadLocalConfig()
	if err != nil {
		return err
	}
	cfg.AppID = appID
	cfg.AppSecret = appSecret
	return saveLocalConfig(cfg)
}

func LoadLocalApp() (appID, appSecret string, err error) {
	if id := strings.TrimSpace(os.Getenv("FEISHU_APP_ID")); id != "" {
		secret := strings.TrimSpace(os.Getenv("FEISHU_APP_SECRET"))
		if secret != "" {
			return id, secret, nil
		}
	}
	cfg, err := loadLocalConfig()
	if err != nil {
		return "", "", err
	}
	if cfg.AppID == "" || cfg.AppSecret == "" {
		return "", "", fmt.Errorf("app credentials not configured, run `larkctl init`")
	}
	return cfg.AppID, cfg.AppSecret, nil
}

func SaveLocalTokens(accessToken, refreshToken string, expireAt time.Time) error {
	cfg, err := loadLocalConfig()
	if err != nil {
		return err
	}
	cfg.AccessToken = accessToken
	cfg.RefreshToken = refreshToken
	cfg.AccessExpireAt = expireAt.Format(time.RFC3339)
	return saveLocalConfig(cfg)
}

func LoadLocalTokens() (accessToken, refreshToken string, expireAt time.Time, err error) {
	cfg, err := loadLocalConfig()
	if err != nil {
		return "", "", time.Time{}, err
	}
	if cfg.AccessToken == "" {
		return "", "", time.Time{}, fmt.Errorf("not logged in, run `larkctl login`")
	}
	expireAt, _ = time.Parse(time.RFC3339, cfg.AccessExpireAt)
	return cfg.AccessToken, cfg.RefreshToken, expireAt, nil
}

func DeleteLocalTokens() error {
	cfg, err := loadLocalConfig()
	if err != nil {
		return err
	}
	cfg.AccessToken = ""
	cfg.RefreshToken = ""
	cfg.AccessExpireAt = ""
	return saveLocalConfig(cfg)
}

func IsLocalMode() bool {
	// Explicit env var
	if strings.TrimSpace(os.Getenv("LARKCTL_MODE")) == "local" {
		return true
	}
	// If app credentials are configured locally
	cfg, err := loadLocalConfig()
	if err != nil {
		return false
	}
	return cfg.AppID != "" && cfg.AppSecret != ""
}
