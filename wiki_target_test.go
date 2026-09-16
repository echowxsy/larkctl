package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func wikiNodeResp(objType, objToken, title string) any {
	return map[string]any{"node": map[string]any{
		"obj_type": objType, "obj_token": objToken, "title": title,
	}}
}

// The docx endpoints answer 1770002 "not found" for a wiki node that holds an
// uploaded file. That reads like a permission or link problem, so surface the
// real cause and the command that does work.
func TestExplainDocTargetFileNode(t *testing.T) {
	t.Parallel()
	m := &mockClient{getWikiNodeResp: wikiNodeResp("file", "FQgub123", "算法说明.md")}
	orig := errors.New("gateway error [feishu_error] (status=403): feishu code=1770002 msg=not found")

	got := explainDocTarget(context.Background(), m, "RUAx123", orig)
	if got == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"算法说明.md", "obj_type=file", "drive download FQgub123"} {
		if !strings.Contains(got.Error(), want) {
			t.Errorf("error %q missing %q", got, want)
		}
	}
}

func TestExplainDocTargetSheetAndBitable(t *testing.T) {
	t.Parallel()
	orig := errors.New("feishu code=1770002 msg=not found")
	for _, tc := range []struct{ objType, want string }{
		{"sheet", "sheets meta"},
		{"bitable", "bitable tables"},
		{"mindnote", "obj_type=mindnote"},
	} {
		m := &mockClient{getWikiNodeResp: wikiNodeResp(tc.objType, "tok", "n")}
		got := explainDocTarget(context.Background(), m, "in", orig)
		if got == nil || !strings.Contains(got.Error(), tc.want) {
			t.Errorf("objType=%s: got %v, want mention of %q", tc.objType, got, tc.want)
		}
	}
}

func TestExplainDocTargetPassesThrough(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	if got := explainDocTarget(ctx, &mockClient{}, "in", nil); got != nil {
		t.Errorf("nil error must stay nil, got %v", got)
	}

	// Unrelated failures must not be rewritten.
	other := errors.New("feishu code=99991663 msg=permission denied")
	m := &mockClient{getWikiNodeResp: wikiNodeResp("file", "tok", "n")}
	if got := explainDocTarget(ctx, m, "in", other); got != other {
		t.Errorf("unrelated error rewritten: %v", got)
	}

	// A real document that 404s keeps the original error.
	docx := &mockClient{getWikiNodeResp: wikiNodeResp("docx", "tok", "n")}
	notFound := errors.New("feishu code=1770002 msg=not found")
	if got := explainDocTarget(ctx, docx, "in", notFound); got != notFound {
		t.Errorf("docx node error rewritten: %v", got)
	}

	// No wiki node at all: nothing extra is known, so keep the original.
	none := &mockClient{getWikiNodeResp: map[string]any{}}
	if got := explainDocTarget(ctx, none, "in", notFound); got != notFound {
		t.Errorf("non-wiki error rewritten: %v", got)
	}
}
