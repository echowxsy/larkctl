package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func TestResolveGatewayURLUsesConfigWhenEnvEmpty(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("FEISHU_GATEWAY_URL", "")

	if err := SaveGatewayURL("https://larkctl.example.com/"); err != nil {
		t.Fatalf("SaveGatewayURL() error = %v", err)
	}

	gatewayURL = ""
	if err := resolveGatewayURL(nil); err != nil {
		t.Fatalf("resolveGatewayURL() error = %v", err)
	}
	if gatewayURL != "https://larkctl.example.com" {
		t.Fatalf("gatewayURL = %q, want %q", gatewayURL, "https://larkctl.example.com")
	}
}

func TestResolveGatewayURLPrefersEnvOverConfig(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("FEISHU_GATEWAY_URL", "https://from-env.example.com/")

	if err := SaveGatewayURL("https://from-config.example.com"); err != nil {
		t.Fatalf("SaveGatewayURL() error = %v", err)
	}

	gatewayURL = ""
	if err := resolveGatewayURL(nil); err != nil {
		t.Fatalf("resolveGatewayURL() error = %v", err)
	}
	if gatewayURL != "https://from-env.example.com" {
		t.Fatalf("gatewayURL = %q, want %q", gatewayURL, "https://from-env.example.com")
	}
}

func TestResolveGatewayURLPrefersFlagOverEnvAndConfig(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("FEISHU_GATEWAY_URL", "https://from-env.example.com")

	if err := SaveGatewayURL("https://from-config.example.com"); err != nil {
		t.Fatalf("SaveGatewayURL() error = %v", err)
	}

	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().StringVar(&gatewayURL, "gateway-url", "", "gateway URL")
	if err := cmd.Flags().Set("gateway-url", "https://from-flag.example.com/"); err != nil {
		t.Fatalf("set flag: %v", err)
	}

	if err := resolveGatewayURL(cmd); err != nil {
		t.Fatalf("resolveGatewayURL() error = %v", err)
	}
	if gatewayURL != "https://from-flag.example.com" {
		t.Fatalf("gatewayURL = %q, want %q", gatewayURL, "https://from-flag.example.com")
	}
}

// --- local_paths.go tests ---

func TestResolveLarkHomeDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	got, err := resolveLarkHomeDir()
	if err != nil {
		t.Fatalf("resolveLarkHomeDir() error = %v", err)
	}
	want := filepath.Join(tmp, ".lark")
	if got != want {
		t.Fatalf("resolveLarkHomeDir() = %q, want %q", got, want)
	}
}

func TestResolveLarkConfigPath(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	got, err := resolveLarkConfigPath()
	if err != nil {
		t.Fatalf("resolveLarkConfigPath() error = %v", err)
	}
	want := filepath.Join(tmp, ".lark", "config.json")
	if got != want {
		t.Fatalf("resolveLarkConfigPath() = %q, want %q", got, want)
	}
}

func TestWriteFileAtomicPaths(t *testing.T) {
	t.Run("creates file and directories", func(t *testing.T) {
		tmp := t.TempDir()
		path := filepath.Join(tmp, "sub", "dir", "test.txt")

		data := []byte("hello world")
		if err := writeFileAtomic(path, data, 0o644); err != nil {
			t.Fatalf("writeFileAtomic() error = %v", err)
		}

		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile() error = %v", err)
		}
		if string(got) != "hello world" {
			t.Fatalf("file content = %q, want %q", string(got), "hello world")
		}
	})

	t.Run("overwrites existing file", func(t *testing.T) {
		tmp := t.TempDir()
		path := filepath.Join(tmp, "test.txt")

		if err := writeFileAtomic(path, []byte("first"), 0o644); err != nil {
			t.Fatalf("first write error = %v", err)
		}
		if err := writeFileAtomic(path, []byte("second"), 0o644); err != nil {
			t.Fatalf("second write error = %v", err)
		}

		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile() error = %v", err)
		}
		if string(got) != "second" {
			t.Fatalf("file content = %q, want %q", string(got), "second")
		}
	})

	t.Run("sets file permissions", func(t *testing.T) {
		tmp := t.TempDir()
		path := filepath.Join(tmp, "perm.txt")

		if err := writeFileAtomic(path, []byte("data"), 0o600); err != nil {
			t.Fatalf("writeFileAtomic() error = %v", err)
		}

		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat() error = %v", err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("file perm = %o, want %o", info.Mode().Perm(), 0o600)
		}
	})
}

// --- main.go utility function tests ---

func TestBuildScopes(t *testing.T) {
	t.Run("includes base scopes", func(t *testing.T) {
		result := buildScopes()
		for _, s := range strings.Fields(baseScopes) {
			if !strings.Contains(result, s) {
				t.Fatalf("buildScopes() missing base scope %q", s)
			}
		}
	})

	t.Run("includes group scopes", func(t *testing.T) {
		result := buildScopes("calendar")
		if !strings.Contains(result, "calendar:calendar") {
			t.Fatalf("buildScopes(calendar) missing calendar scope")
		}
	})

	t.Run("unknown group ignored", func(t *testing.T) {
		result := buildScopes("nonexistent")
		// Should still have base scopes
		if result != buildScopes() {
			t.Fatalf("unknown group changed result")
		}
	})

	t.Run("no duplicate scopes", func(t *testing.T) {
		result := buildScopes("docs", "docs")
		fields := strings.Fields(result)
		seen := map[string]bool{}
		for _, f := range fields {
			if seen[f] {
				t.Fatalf("duplicate scope: %q", f)
			}
			seen[f] = true
		}
	})
}

func TestAllScopeGroupNames(t *testing.T) {
	names := allScopeGroupNames()
	if len(names) == 0 {
		t.Fatalf("allScopeGroupNames() returned empty")
	}

	// Verify sorted
	if !sort.StringsAreSorted(names) {
		t.Fatalf("allScopeGroupNames() not sorted: %v", names)
	}

	// Verify known groups exist
	nameSet := map[string]bool{}
	for _, n := range names {
		nameSet[n] = true
	}
	for _, expected := range []string{"docs", "calendar", "task"} {
		if !nameSet[expected] {
			t.Fatalf("allScopeGroupNames() missing %q", expected)
		}
	}
}

func TestEnvOrDefault(t *testing.T) {
	t.Run("returns env value when set", func(t *testing.T) {
		t.Setenv("TEST_ENV_OR_DEFAULT_KEY", "from-env")
		got := envOrDefault("TEST_ENV_OR_DEFAULT_KEY", "fallback")
		if got != "from-env" {
			t.Fatalf("envOrDefault() = %q, want %q", got, "from-env")
		}
	})

	t.Run("returns default when env empty", func(t *testing.T) {
		t.Setenv("TEST_ENV_OR_DEFAULT_KEY", "")
		got := envOrDefault("TEST_ENV_OR_DEFAULT_KEY", "fallback")
		if got != "fallback" {
			t.Fatalf("envOrDefault() = %q, want %q", got, "fallback")
		}
	})

	t.Run("returns default when env whitespace", func(t *testing.T) {
		t.Setenv("TEST_ENV_OR_DEFAULT_KEY", "   ")
		got := envOrDefault("TEST_ENV_OR_DEFAULT_KEY", "fallback")
		if got != "fallback" {
			t.Fatalf("envOrDefault() = %q, want %q", got, "fallback")
		}
	})

	t.Run("returns default for unset env", func(t *testing.T) {
		// Use a key unlikely to be set
		got := envOrDefault("LARKCTL_TEST_DEFINITELY_NOT_SET_XYZ", "mydefault")
		if got != "mydefault" {
			t.Fatalf("envOrDefault() = %q, want %q", got, "mydefault")
		}
	})
}

func TestParseToUnix(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		checkFn  func(string) bool
		wantSame bool // if true, output should equal input
	}{
		{name: "already unix", input: "1700000000", wantSame: true},
		{name: "empty string", input: "", wantSame: true},
		{name: "whitespace", input: "  ", checkFn: func(s string) bool { return s == "" }},
		{name: "date only", input: "2026-01-15", checkFn: func(s string) bool {
			// Should parse to a valid unix timestamp
			return len(s) > 0 && s[0] >= '0' && s[0] <= '9'
		}},
		{name: "datetime space", input: "2026-01-15 10:30", checkFn: func(s string) bool {
			return len(s) > 0 && s[0] >= '0' && s[0] <= '9'
		}},
		{name: "datetime T", input: "2026-01-15T10:30", checkFn: func(s string) bool {
			return len(s) > 0 && s[0] >= '0' && s[0] <= '9'
		}},
		{name: "datetime with seconds", input: "2026-01-15 10:30:45", checkFn: func(s string) bool {
			return len(s) > 0 && s[0] >= '0' && s[0] <= '9'
		}},
		{name: "invalid format passthrough", input: "not-a-date", wantSame: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseToUnix(tc.input)
			if tc.wantSame && got != strings.TrimSpace(tc.input) {
				t.Fatalf("parseToUnix(%q) = %q, want %q", tc.input, got, strings.TrimSpace(tc.input))
			}
			if tc.checkFn != nil && !tc.checkFn(got) {
				t.Fatalf("parseToUnix(%q) = %q, check failed", tc.input, got)
			}
		})
	}
}

func TestParseToRFC3339(t *testing.T) {
	t.Run("already RFC3339", func(t *testing.T) {
		input := "2026-01-15T10:30:00+08:00"
		got := parseToRFC3339(input)
		if got != input {
			t.Fatalf("parseToRFC3339(%q) = %q, want same", input, got)
		}
	})

	t.Run("UTC RFC3339", func(t *testing.T) {
		input := "2026-01-15T10:30:00Z"
		got := parseToRFC3339(input)
		if got != input {
			t.Fatalf("parseToRFC3339(%q) = %q, want same", input, got)
		}
	})

	t.Run("date only", func(t *testing.T) {
		got := parseToRFC3339("2026-01-15")
		// Should contain T and timezone info
		if !strings.Contains(got, "T") {
			t.Fatalf("parseToRFC3339(date) = %q, expected RFC3339 format", got)
		}
		// Parse result to verify it's valid RFC3339
		if _, err := time.Parse(time.RFC3339, got); err != nil {
			t.Fatalf("parseToRFC3339(date) = %q, not valid RFC3339: %v", got, err)
		}
	})

	t.Run("datetime space", func(t *testing.T) {
		got := parseToRFC3339("2026-01-15 10:30")
		if _, err := time.Parse(time.RFC3339, got); err != nil {
			t.Fatalf("parseToRFC3339() = %q, not valid RFC3339: %v", got, err)
		}
	})

	t.Run("invalid passthrough", func(t *testing.T) {
		got := parseToRFC3339("garbage")
		if got != "garbage" {
			t.Fatalf("parseToRFC3339(garbage) = %q, want %q", got, "garbage")
		}
	})
}

func TestUnixToRFC3339(t *testing.T) {
	t.Run("valid timestamp", func(t *testing.T) {
		got := unixToRFC3339("1700000000")
		parsed, err := time.Parse(time.RFC3339, got)
		if err != nil {
			t.Fatalf("unixToRFC3339() = %q, not valid RFC3339: %v", got, err)
		}
		if parsed.Unix() != 1700000000 {
			t.Fatalf("parsed unix = %d, want 1700000000", parsed.Unix())
		}
	})

	t.Run("invalid passthrough", func(t *testing.T) {
		got := unixToRFC3339("notanumber")
		if got != "notanumber" {
			t.Fatalf("unixToRFC3339(notanumber) = %q, want passthrough", got)
		}
	})

	t.Run("zero timestamp", func(t *testing.T) {
		got := unixToRFC3339("0")
		if _, err := time.Parse(time.RFC3339, got); err != nil {
			t.Fatalf("unixToRFC3339(0) = %q, not valid RFC3339: %v", got, err)
		}
	})
}

func TestResolveTimeRange(t *testing.T) {
	t.Run("both start and end provided", func(t *testing.T) {
		start, end := resolveTimeRange("1700000000", "1700100000", 7)
		if start != "1700000000" {
			t.Fatalf("start = %q, want %q", start, "1700000000")
		}
		if end != "1700100000" {
			t.Fatalf("end = %q, want %q", end, "1700100000")
		}
	})

	t.Run("no start no end uses days", func(t *testing.T) {
		start, end := resolveTimeRange("", "", 7)
		// Both should be valid unix timestamps
		if len(start) == 0 || start[0] < '0' || start[0] > '9' {
			t.Fatalf("start = %q, not a unix timestamp", start)
		}
		if len(end) == 0 || end[0] < '0' || end[0] > '9' {
			t.Fatalf("end = %q, not a unix timestamp", end)
		}
		// Parse and verify end > start
		var s, e int64
		fmt.Sscanf(start, "%d", &s)
		fmt.Sscanf(end, "%d", &e)
		if e <= s {
			t.Fatalf("end (%d) should be > start (%d)", e, s)
		}
	})

	t.Run("only start provided", func(t *testing.T) {
		start, end := resolveTimeRange("1700000000", "", 7)
		if start != "1700000000" {
			t.Fatalf("start = %q, want %q", start, "1700000000")
		}
		if len(end) == 0 || end[0] < '0' || end[0] > '9' {
			t.Fatalf("end = %q, not a unix timestamp", end)
		}
	})

	t.Run("only end provided", func(t *testing.T) {
		start, end := resolveTimeRange("", "1700100000", 7)
		if len(start) == 0 || start[0] < '0' || start[0] > '9' {
			t.Fatalf("start = %q, not a unix timestamp", start)
		}
		if end != "1700100000" {
			t.Fatalf("end = %q, want %q", end, "1700100000")
		}
	})
}

func TestHasAllScopes(t *testing.T) {
	cases := []struct {
		name string
		have string
		need string
		want bool
	}{
		{name: "all present", have: "a b c d", need: "a c", want: true},
		{name: "missing one", have: "a b", need: "a c", want: false},
		{name: "empty need", have: "a b c", need: "", want: true},
		{name: "empty have", have: "", need: "a", want: false},
		{name: "both empty", have: "", need: "", want: true},
		{name: "exact match", have: "a b c", need: "a b c", want: true},
		{name: "superset", have: "a b c d e", need: "b d", want: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := hasAllScopes(tc.have, tc.need)
			if got != tc.want {
				t.Fatalf("hasAllScopes(%q, %q) = %v, want %v", tc.have, tc.need, got, tc.want)
			}
		})
	}
}

func TestExtractEventID(t *testing.T) {
	cases := []struct {
		name string
		data any
		want string
	}{
		{
			name: "valid event",
			data: map[string]any{"event": map[string]any{"event_id": "evt_123"}},
			want: "evt_123",
		},
		{name: "nil data", data: nil, want: ""},
		{name: "no event key", data: map[string]any{"foo": "bar"}, want: ""},
		{
			name: "event without event_id",
			data: map[string]any{"event": map[string]any{"other": "val"}},
			want: "",
		},
		{name: "wrong type", data: "string", want: ""},
		{
			name: "event not map",
			data: map[string]any{"event": "not-a-map"},
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractEventID(tc.data)
			if got != tc.want {
				t.Fatalf("extractEventID() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestReadJSONInputPaths(t *testing.T) {
	t.Run("read from file arg", func(t *testing.T) {
		tmp := t.TempDir()
		path := filepath.Join(tmp, "input.json")
		if err := os.WriteFile(path, []byte(`{"key":"value"}`), 0o644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		var out map[string]any
		if err := readJSONInput([]string{path}, 0, &out); err != nil {
			t.Fatalf("readJSONInput() error = %v", err)
		}
		if out["key"] != "value" {
			t.Fatalf("out[key] = %v, want %q", out["key"], "value")
		}
	})

	t.Run("file not found", func(t *testing.T) {
		var out map[string]any
		err := readJSONInput([]string{"/nonexistent/path.json"}, 0, &out)
		if err == nil {
			t.Fatalf("readJSONInput() expected error for missing file")
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		tmp := t.TempDir()
		path := filepath.Join(tmp, "bad.json")
		if err := os.WriteFile(path, []byte(`not json`), 0o644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		var out map[string]any
		err := readJSONInput([]string{path}, 0, &out)
		if err == nil {
			t.Fatalf("readJSONInput() expected error for invalid JSON")
		}
	})

	t.Run("read array JSON", func(t *testing.T) {
		tmp := t.TempDir()
		path := filepath.Join(tmp, "array.json")
		if err := os.WriteFile(path, []byte(`[1,2,3]`), 0o644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		var out []int
		if err := readJSONInput([]string{path}, 0, &out); err != nil {
			t.Fatalf("readJSONInput() error = %v", err)
		}
		if len(out) != 3 || out[0] != 1 {
			t.Fatalf("out = %v, want [1,2,3]", out)
		}
	})
}
