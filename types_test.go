package main

import (
	"encoding/json"
	"testing"
)

func TestInferDocumentType(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		explicit string
		want     string
		wantErr  bool
	}{
		{name: "auto wiki", input: "https://foo/wiki/abc", explicit: "auto", want: "wiki"},
		{name: "auto doc", input: "ERIldO", explicit: "auto", want: "document"},
		{name: "explicit wiki", input: "ERIldO", explicit: "wiki", want: "wiki"},
		{name: "invalid explicit", input: "ERIldO", explicit: "sheet", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := inferDocumentType(tc.input, tc.explicit)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("inferDocumentType() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNormalizeBaseURL(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{name: "empty returns default", raw: "", want: defaultGatewayURL},
		{name: "whitespace only returns default", raw: "   ", want: defaultGatewayURL},
		{name: "trailing slash removed", raw: "https://example.com/", want: "https://example.com"},
		{name: "multiple trailing slashes", raw: "https://example.com///", want: "https://example.com"},
		{name: "no trailing slash unchanged", raw: "https://example.com", want: "https://example.com"},
		{name: "with path trailing slash", raw: "https://example.com/api/", want: "https://example.com/api"},
		{name: "leading/trailing whitespace trimmed", raw: "  https://example.com/  ", want: "https://example.com"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeBaseURL(tc.raw)
			if got != tc.want {
				t.Fatalf("normalizeBaseURL(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestPickString(t *testing.T) {
	cases := []struct {
		name string
		data map[string]any
		keys []string
		want string
	}{
		{name: "first key found", data: map[string]any{"a": "hello"}, keys: []string{"a", "b"}, want: "hello"},
		{name: "second key found", data: map[string]any{"b": "world"}, keys: []string{"a", "b"}, want: "world"},
		{name: "no key found", data: map[string]any{"c": "value"}, keys: []string{"a", "b"}, want: ""},
		{name: "empty map", data: map[string]any{}, keys: []string{"a"}, want: ""},
		{name: "nil value skipped", data: map[string]any{"a": nil, "b": "ok"}, keys: []string{"a", "b"}, want: "ok"},
		{name: "empty string skipped", data: map[string]any{"a": "", "b": "ok"}, keys: []string{"a", "b"}, want: "ok"},
		{name: "non-string value", data: map[string]any{"a": 42}, keys: []string{"a"}, want: "42"},
		{name: "whitespace only skipped", data: map[string]any{"a": "   ", "b": "ok"}, keys: []string{"a", "b"}, want: "ok"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := pickString(tc.data, tc.keys...)
			if got != tc.want {
				t.Fatalf("pickString() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRandHex(t *testing.T) {
	t.Run("default length when zero", func(t *testing.T) {
		hex, err := randHex(0)
		if err != nil {
			t.Fatalf("randHex(0) error = %v", err)
		}
		if len(hex) != 32 { // 16 bytes = 32 hex chars
			t.Fatalf("randHex(0) len = %d, want 32", len(hex))
		}
	})

	t.Run("negative uses default", func(t *testing.T) {
		hex, err := randHex(-5)
		if err != nil {
			t.Fatalf("randHex(-5) error = %v", err)
		}
		if len(hex) != 32 {
			t.Fatalf("randHex(-5) len = %d, want 32", len(hex))
		}
	})

	t.Run("specific length", func(t *testing.T) {
		hex, err := randHex(8)
		if err != nil {
			t.Fatalf("randHex(8) error = %v", err)
		}
		if len(hex) != 16 { // 8 bytes = 16 hex chars
			t.Fatalf("randHex(8) len = %d, want 16", len(hex))
		}
	})

	t.Run("uniqueness", func(t *testing.T) {
		a, _ := randHex(16)
		b, _ := randHex(16)
		if a == b {
			t.Fatalf("two calls returned same value: %q", a)
		}
	})
}

func TestPickInt(t *testing.T) {
	cases := []struct {
		name string
		data map[string]any
		keys []string
		want int
	}{
		{name: "first key found", data: map[string]any{"a": 42}, keys: []string{"a", "b"}, want: 42},
		{name: "second key found", data: map[string]any{"b": 7}, keys: []string{"a", "b"}, want: 7},
		{name: "no key found", data: map[string]any{}, keys: []string{"a"}, want: 0},
		{name: "zero skipped", data: map[string]any{"a": 0, "b": 5}, keys: []string{"a", "b"}, want: 5},
		{name: "float64 value", data: map[string]any{"a": 3.7}, keys: []string{"a"}, want: 3},
		{name: "string value", data: map[string]any{"a": "99"}, keys: []string{"a"}, want: 99},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := pickInt(tc.data, tc.keys...)
			if got != tc.want {
				t.Fatalf("pickInt() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestIntFromAny(t *testing.T) {
	cases := []struct {
		name string
		val  any
		want int
	}{
		{name: "int", val: 42, want: 42},
		{name: "int32", val: int32(10), want: 10},
		{name: "int64", val: int64(100), want: 100},
		{name: "float64", val: 3.14, want: 3},
		{name: "json.Number", val: json.Number("55"), want: 55},
		{name: "string numeric", val: "123", want: 123},
		{name: "string with spaces", val: "  456  ", want: 456},
		{name: "empty string", val: "", want: 0},
		{name: "non-numeric string", val: "abc", want: 0},
		{name: "nil", val: nil, want: 0},
		{name: "bool", val: true, want: 0},
		{name: "slice", val: []int{1}, want: 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := intFromAny(tc.val)
			if got != tc.want {
				t.Fatalf("intFromAny(%v) = %d, want %d", tc.val, got, tc.want)
			}
		})
	}
}

func TestMustPathEscape(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  string
	}{
		{name: "simple string", value: "hello", want: "hello"},
		{name: "with slash", value: "a/b", want: "a%2Fb"},
		{name: "with space", value: "hello world", want: "hello%20world"},
		{name: "empty string", value: "", want: ""},
		{name: "special chars", value: "doc?id=1", want: "doc%3Fid=1"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mustPathEscape(tc.value)
			if got != tc.want {
				t.Fatalf("mustPathEscape(%q) = %q, want %q", tc.value, got, tc.want)
			}
		})
	}
}

func TestExtractToken(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{name: "full URL", input: "https://feishu.cn/docs/abc123", want: "abc123"},
		{name: "URL with trailing slash", input: "https://feishu.cn/docs/abc123/", want: "abc123"},
		{name: "wiki URL", input: "https://feishu.cn/wiki/XYZ789", want: "XYZ789"},
		{name: "plain token", input: "abc123", want: "abc123"},
		{name: "empty string", input: "", want: ""},
		{name: "whitespace only", input: "   ", want: ""},
		{name: "path without host", input: "abc/def/ghi", want: "ghi"},
		{name: "single segment URL", input: "https://feishu.cn/token", want: "token"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractToken(tc.input)
			if got != tc.want {
				t.Fatalf("extractToken(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestLooksLikeUserID(t *testing.T) {
	cases := []struct {
		name string
		s    string
		want bool
	}{
		{name: "open_id prefix", s: "ou_abc123", want: true},
		{name: "union_id prefix", s: "on_abc123", want: true},
		{name: "numeric string", s: "12345678", want: true},
		{name: "empty string", s: "", want: false},
		{name: "name string", s: "John", want: false},
		{name: "mixed alphanumeric", s: "abc123", want: false},
		{name: "single digit", s: "0", want: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := looksLikeUserID(tc.s)
			if got != tc.want {
				t.Fatalf("looksLikeUserID(%q) = %v, want %v", tc.s, got, tc.want)
			}
		})
	}
}
