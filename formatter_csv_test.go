package main

import (
	"bytes"
	"encoding/csv"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateRunesKeepsValidUTF8(t *testing.T) {
	t.Parallel()
	// 60 CJK runes: byte-slicing at 57 would cut a rune in half.
	s := strings.Repeat("分", 100)
	got := truncateRunes(s, 60)
	if !utf8.ValidString(got) {
		t.Fatalf("truncateRunes produced invalid UTF-8: %q", got)
	}
	if n := utf8.RuneCountInString(got); n != 60 {
		t.Errorf("rune count = %d, want 60", n)
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("missing ellipsis: %q", got)
	}
}

func TestTruncateRunesShortInputUnchanged(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"", "短", strings.Repeat("a", 60)} {
		if got := truncateRunes(s, 60); got != s {
			t.Errorf("truncateRunes(%q) = %q, want unchanged", s, got)
		}
	}
}

func TestFormatValueFullDoesNotTruncate(t *testing.T) {
	t.Parallel()
	v := map[string]any{"k": strings.Repeat("值", 200)}
	got := formatValueFull(v)
	if strings.HasSuffix(got, "...") {
		t.Errorf("formatValueFull truncated: %q", got[len(got)-20:])
	}
	if !utf8.ValidString(got) {
		t.Error("formatValueFull produced invalid UTF-8")
	}
}

// CSV is machine-readable output: nested objects become their own columns and
// nothing is truncated, so the result must round-trip through a CSV parser.
func TestPrintCSVExpandsNestedObjects(t *testing.T) {
	t.Parallel()
	data := map[string]any{"items": []any{
		map[string]any{
			"record_id": "rec1",
			"fields":    map[string]any{"项目名称": strings.Repeat("长", 120), "总分": float64(88)},
		},
		map[string]any{
			"record_id": "rec2",
			"fields":    map[string]any{"项目名称": "短名", "评委姓名": "某人"},
		},
	}}

	var buf bytes.Buffer
	printCSV(&buf, data)

	if !utf8.Valid(buf.Bytes()) {
		t.Fatal("CSV output is not valid UTF-8")
	}
	records, err := csv.NewReader(bytes.NewReader(buf.Bytes())).ReadAll()
	if err != nil {
		t.Fatalf("CSV does not parse: %v", err)
	}
	header := strings.Join(records[0], ",")
	for _, want := range []string{"fields.项目名称", "fields.总分", "fields.评委姓名"} {
		if !strings.Contains(header, want) {
			t.Errorf("header %q missing column %q", header, want)
		}
	}
	if strings.Contains(strings.Join(records[1], ","), "...") {
		t.Error("CSV truncated a value")
	}
	// A column missing from one row must still be present and empty.
	if len(records[1]) != len(records[0]) || len(records[2]) != len(records[0]) {
		t.Errorf("ragged rows: header=%d r1=%d r2=%d", len(records[0]), len(records[1]), len(records[2]))
	}
}

func TestFormatValueStillTruncatesForTable(t *testing.T) {
	t.Parallel()
	got := formatValue(map[string]any{"k": strings.Repeat("x", 200)})
	if utf8.RuneCountInString(got) > 60 {
		t.Errorf("table value not truncated: %d runes", utf8.RuneCountInString(got))
	}
}
