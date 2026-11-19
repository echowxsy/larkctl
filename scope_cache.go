package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// The gateway answers /v1/session/upgrade by comparing the requested scopes
// with the scopes recorded on the session, and a session's scopes only ever
// grow (a reauth merges new scopes in; a fresh login is a new session). So
// once the gateway has confirmed a set of scopes for a session token, the
// answer for any subset stays "no upgrade needed" for that token's lifetime.
// Caching it saves a round trip on every command, which for scripts calling
// larkctl in a loop was the gateway's single most frequent request.
//
// The file holds one entry, for the current token, keyed by a hash so the
// token itself is not written a second time.
const scopeCacheFileName = "scope-cache.json"

type scopeCacheEntry struct {
	Session string `json:"session"`
	Scopes  string `json:"scopes"`
}

func defaultScopeCachePath() string {
	root, err := resolveLarkHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(root, scopeCacheFileName)
}

func scopeCacheKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:16])
}

// cachedScopesCover reports whether the cache already confirms scopes for token.
func cachedScopesCover(path, token, need string) bool {
	if path == "" || token == "" {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var e scopeCacheEntry
	if json.Unmarshal(data, &e) != nil || e.Session != scopeCacheKey(token) || strings.TrimSpace(e.Scopes) == "" {
		return false
	}
	return hasAllScopes(e.Scopes, need)
}

// storeCachedScopes records the scopes the gateway confirmed for token. Errors
// are ignored: the cache only saves a round trip.
func storeCachedScopes(path, token, scopes string) {
	if path == "" || token == "" || strings.TrimSpace(scopes) == "" {
		return
	}
	data, err := json.Marshal(scopeCacheEntry{Session: scopeCacheKey(token), Scopes: scopes})
	if err != nil {
		return
	}
	_ = writeFileAtomic(path, data, 0o600)
}
