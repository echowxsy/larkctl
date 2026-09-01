package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseMarkdownWithIDsPreservesComplexBlocks(t *testing.T) {
	t.Parallel()

	md := strings.Join([]string{
		"<!-- bid:img-1 -->",
		"![image](feishu-media://img-token)",
		"",
		"<!-- bid:file-1 -->",
		"[spec.pdf](feishu-file://file-token)",
		"",
		"<!-- bid:table-1 -->",
		"| A | B |",
		"| --- | --- |",
		"| 1 | 2 |",
		"",
		"<!-- bid:text-1 -->",
		"Hello **world**",
	}, "\n")

	entries, err := parseMarkdownWithIDs(md)
	if err != nil {
		t.Fatalf("parseMarkdownWithIDs() error = %v", err)
	}
	if len(entries) != 4 {
		t.Fatalf("entries = %d, want 4", len(entries))
	}
	if entries[0].BlockType != btImage || entries[0].Comparable != "image:img-token" || entries[0].Body != nil {
		t.Fatalf("image entry = %+v", entries[0])
	}
	if entries[1].BlockType != btFile || entries[1].Comparable != "file:spec.pdf\tfile-token" || entries[1].Body != nil {
		t.Fatalf("file entry = %+v", entries[1])
	}
	if entries[2].BlockType != btTable || entries[2].Body != nil {
		t.Fatalf("table entry = %+v", entries[2])
	}
	if got, want := entries[2].Comparable, "| A | B |\n| --- | --- |\n| 1 | 2 |"; got != want {
		t.Fatalf("table comparable = %q, want %q", got, want)
	}
	if got, want := entries[3].Comparable, "Hello **world**"; got != want {
		t.Fatalf("text comparable = %q, want %q", got, want)
	}
}

func TestParseMarkdownWithIDsNestedLists(t *testing.T) {
	t.Parallel()

	entries, err := parseMarkdownWithIDs(strings.Join([]string{
		"- parent",
		"  - child1",
		"  - child2",
		"    - grandchild",
		"- sibling",
	}, "\n"))
	if err != nil {
		t.Fatalf("parseMarkdownWithIDs() error = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2 (parent + sibling)", len(entries))
	}
	if entries[0].Comparable != "parent" {
		t.Errorf("entries[0].Comparable = %q, want %q", entries[0].Comparable, "parent")
	}
	if len(entries[0].Children) != 2 {
		t.Fatalf("entries[0].Children = %d, want 2", len(entries[0].Children))
	}
	if entries[0].Children[0].Comparable != "child1" {
		t.Errorf("child[0].Comparable = %q, want %q", entries[0].Children[0].Comparable, "child1")
	}
	if entries[0].Children[1].Comparable != "child2" {
		t.Errorf("child[1].Comparable = %q, want %q", entries[0].Children[1].Comparable, "child2")
	}
	if len(entries[0].Children[1].Children) != 1 {
		t.Fatalf("child[1].Children = %d, want 1", len(entries[0].Children[1].Children))
	}
	if entries[0].Children[1].Children[0].Comparable != "grandchild" {
		t.Errorf("grandchild.Comparable = %q, want %q", entries[0].Children[1].Children[0].Comparable, "grandchild")
	}
	if entries[1].Comparable != "sibling" {
		t.Errorf("entries[1].Comparable = %q, want %q", entries[1].Comparable, "sibling")
	}
}

func TestLocalClientCreateDocumentBlocksDefaultsToRoot(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotQuery string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"ok":true}}`))
	}))
	defer srv.Close()

	client := NewLocalClient("app-id", "app-secret")
	client.baseURL = srv.URL
	client.SetTokens("token", "refresh", time.Now().Add(time.Hour))

	if _, err := client.CreateDocumentBlocks(context.Background(), "doc-123", "", map[string]any{
		"children": []any{},
		"index":    -1,
	}); err != nil {
		t.Fatalf("CreateDocumentBlocks() error = %v", err)
	}

	if gotPath != "/docx/v1/documents/doc-123/blocks/doc-123/children" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotQuery != "document_revision_id=-1" {
		t.Fatalf("query = %q", gotQuery)
	}
}

// --- renderElements tests ---

func TestRenderElements(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		elements []element
		want     string
	}{
		{
			name:     "empty elements",
			elements: nil,
			want:     "",
		},
		{
			name: "plain text",
			elements: []element{
				{TextRun: &textRun{Content: "hello world"}},
			},
			want: "hello world",
		},
		{
			name: "nil text_run skipped",
			elements: []element{
				{TextRun: nil},
				{TextRun: &textRun{Content: "visible"}},
			},
			want: "visible",
		},
		{
			name: "bold",
			elements: []element{
				{TextRun: &textRun{Content: "bold", Style: textRunStyle{Bold: true}}},
			},
			want: "**bold**",
		},
		{
			name: "italic",
			elements: []element{
				{TextRun: &textRun{Content: "italic", Style: textRunStyle{Italic: true}}},
			},
			want: "*italic*",
		},
		{
			name: "strikethrough",
			elements: []element{
				{TextRun: &textRun{Content: "deleted", Style: textRunStyle{Strikethrough: true}}},
			},
			want: "~~deleted~~",
		},
		{
			name: "inline code",
			elements: []element{
				{TextRun: &textRun{Content: "code", Style: textRunStyle{InlineCode: true}}},
			},
			want: "`code`",
		},
		{
			name: "link",
			elements: []element{
				{TextRun: &textRun{Content: "Google", Style: textRunStyle{Link: &link{URL: "https://google.com"}}}},
			},
			want: "[Google](https://google.com)",
		},
		{
			name: "link with encoded URL",
			elements: []element{
				{TextRun: &textRun{Content: "search", Style: textRunStyle{Link: &link{URL: "https://google.com/search%3Fq%3Dtest"}}}},
			},
			want: "[search](https://google.com/search?q=test)",
		},
		{
			name: "bold + italic combined",
			elements: []element{
				{TextRun: &textRun{Content: "emphasis", Style: textRunStyle{Bold: true, Italic: true}}},
			},
			want: "***emphasis***",
		},
		{
			name: "bold link",
			elements: []element{
				{TextRun: &textRun{Content: "click", Style: textRunStyle{Bold: true, Link: &link{URL: "https://example.com"}}}},
			},
			want: "**[click](https://example.com)**",
		},
		{
			name: "multiple elements",
			elements: []element{
				{TextRun: &textRun{Content: "Hello "}},
				{TextRun: &textRun{Content: "world", Style: textRunStyle{Bold: true}}},
				{TextRun: &textRun{Content: "!"}},
			},
			want: "Hello **world**!",
		},
		{
			name: "inline code ignores other styles",
			elements: []element{
				{TextRun: &textRun{Content: "code", Style: textRunStyle{InlineCode: true, Bold: true}}},
			},
			want: "`code`",
		},
		{
			name: "empty link URL ignored",
			elements: []element{
				{TextRun: &textRun{Content: "text", Style: textRunStyle{Link: &link{URL: ""}}}},
			},
			want: "text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderElements(tt.elements)
			if got != tt.want {
				t.Errorf("renderElements() = %q, want %q", got, tt.want)
			}
		})
	}
}

// --- parseInlineMarkdown tests ---

func TestParseInlineMarkdown(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		check func(t *testing.T, result []any)
	}{
		{
			name:  "plain text",
			input: "hello world",
			check: func(t *testing.T, result []any) {
				if len(result) != 1 {
					t.Fatalf("len = %d, want 1", len(result))
				}
				assertTextRunContent(t, result[0], "hello world")
			},
		},
		{
			name:  "empty string",
			input: "",
			check: func(t *testing.T, result []any) {
				if len(result) != 1 {
					t.Fatalf("len = %d, want 1", len(result))
				}
				assertTextRunContent(t, result[0], "")
			},
		},
		{
			name:  "bold text",
			input: "Hello **world**",
			check: func(t *testing.T, result []any) {
				if len(result) != 2 {
					t.Fatalf("len = %d, want 2", len(result))
				}
				assertTextRunContent(t, result[0], "Hello ")
				assertTextRunContent(t, result[1], "world")
				assertTextRunStyle(t, result[1], "bold", true)
			},
		},
		{
			name:  "inline code",
			input: "use `fmt.Println` here",
			check: func(t *testing.T, result []any) {
				if len(result) != 3 {
					t.Fatalf("len = %d, want 3", len(result))
				}
				assertTextRunContent(t, result[0], "use ")
				assertTextRunContent(t, result[1], "fmt.Println")
				assertTextRunStyle(t, result[1], "inline_code", true)
				assertTextRunContent(t, result[2], " here")
			},
		},
		{
			name:  "link",
			input: "visit [Google](https://google.com) now",
			check: func(t *testing.T, result []any) {
				if len(result) != 3 {
					t.Fatalf("len = %d, want 3", len(result))
				}
				assertTextRunContent(t, result[0], "visit ")
				assertTextRunContent(t, result[1], "Google")
				assertTextRunContent(t, result[2], " now")
			},
		},
		{
			name:  "unclosed bold treated as plain text",
			input: "**unclosed",
			check: func(t *testing.T, result []any) {
				// unclosed ** splits at * boundary, both fragments are plain text
				var full string
				for _, r := range result {
					m := r.(map[string]any)
					tr := m["text_run"].(map[string]any)
					full += tr["content"].(string)
				}
				if full != "**unclosed" {
					t.Errorf("combined content = %q, want %q", full, "**unclosed")
				}
			},
		},
		{
			name:  "unclosed backtick treated as plain text",
			input: "`unclosed",
			check: func(t *testing.T, result []any) {
				if len(result) != 1 {
					t.Fatalf("len = %d, want 1", len(result))
				}
				assertTextRunContent(t, result[0], "`unclosed")
			},
		},
		{
			name:  "incomplete link treated as plain text",
			input: "[text](no-closing-paren",
			check: func(t *testing.T, result []any) {
				if len(result) != 1 {
					t.Fatalf("len = %d, want 1", len(result))
				}
			},
		},
		{
			name:  "multiple bold sections",
			input: "**a** and **b**",
			check: func(t *testing.T, result []any) {
				if len(result) != 3 {
					t.Fatalf("len = %d, want 3", len(result))
				}
				assertTextRunContent(t, result[0], "a")
				assertTextRunStyle(t, result[0], "bold", true)
				assertTextRunContent(t, result[1], " and ")
				assertTextRunContent(t, result[2], "b")
				assertTextRunStyle(t, result[2], "bold", true)
			},
		},
		{
			name:  "italic text",
			input: "this is *italic* text",
			check: func(t *testing.T, result []any) {
				if len(result) != 3 {
					t.Fatalf("len = %d, want 3", len(result))
				}
				assertTextRunContent(t, result[0], "this is ")
				assertTextRunContent(t, result[1], "italic")
				assertTextRunStyle(t, result[1], "italic", true)
				assertTextRunContent(t, result[2], " text")
			},
		},
		{
			name:  "strikethrough text",
			input: "this is ~~deleted~~ text",
			check: func(t *testing.T, result []any) {
				if len(result) != 3 {
					t.Fatalf("len = %d, want 3", len(result))
				}
				assertTextRunContent(t, result[0], "this is ")
				assertTextRunContent(t, result[1], "deleted")
				assertTextRunStyle(t, result[1], "strikethrough", true)
				assertTextRunContent(t, result[2], " text")
			},
		},
		{
			name:  "highlight text",
			input: "this is ==highlighted== text",
			check: func(t *testing.T, result []any) {
				if len(result) != 3 {
					t.Fatalf("len = %d, want 3", len(result))
				}
				assertTextRunContent(t, result[0], "this is ")
				assertTextRunContent(t, result[1], "highlighted")
				assertTextRunStyle(t, result[1], "background_color", 3)
				assertTextRunContent(t, result[2], " text")
			},
		},
		{
			name:  "bold and italic combined",
			input: "***bold+italic***",
			check: func(t *testing.T, result []any) {
				if len(result) != 1 {
					t.Fatalf("len = %d, want 1", len(result))
				}
				assertTextRunContent(t, result[0], "bold+italic")
				assertTextRunStyle(t, result[0], "bold", true)
				assertTextRunStyle(t, result[0], "italic", true)
			},
		},
		{
			name:  "mixed styles in one line",
			input: "**bold** and *italic* and ~~strike~~",
			check: func(t *testing.T, result []any) {
				if len(result) != 5 {
					t.Fatalf("len = %d, want 5", len(result))
				}
				assertTextRunContent(t, result[0], "bold")
				assertTextRunStyle(t, result[0], "bold", true)
				assertTextRunContent(t, result[1], " and ")
				assertTextRunContent(t, result[2], "italic")
				assertTextRunStyle(t, result[2], "italic", true)
				assertTextRunContent(t, result[3], " and ")
				assertTextRunContent(t, result[4], "strike")
				assertTextRunStyle(t, result[4], "strikethrough", true)
			},
		},
		{
			name:  "unclosed italic treated as plain text",
			input: "*unclosed",
			check: func(t *testing.T, result []any) {
				if len(result) != 1 {
					t.Fatalf("len = %d, want 1", len(result))
				}
				assertTextRunContent(t, result[0], "*unclosed")
			},
		},
		{
			name:  "unclosed strikethrough treated as plain text",
			input: "~~unclosed",
			check: func(t *testing.T, result []any) {
				if len(result) != 1 {
					t.Fatalf("len = %d, want 1", len(result))
				}
				assertTextRunContent(t, result[0], "~~unclosed")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseInlineMarkdown(tt.input, 1)
			if err != nil {
				t.Fatalf("parseInlineMarkdown() error = %v", err)
			}
			tt.check(t, result)
		})
	}
}

func assertTextRunContent(t *testing.T, elem any, want string) {
	t.Helper()
	m, ok := elem.(map[string]any)
	if !ok {
		t.Fatalf("element is not map[string]any: %T", elem)
	}
	tr, ok := m["text_run"].(map[string]any)
	if !ok {
		t.Fatalf("missing text_run")
	}
	got, _ := tr["content"].(string)
	if got != want {
		t.Errorf("content = %q, want %q", got, want)
	}
}

func assertTextRunStyle(t *testing.T, elem any, key string, want any) {
	t.Helper()
	m := elem.(map[string]any)
	tr := m["text_run"].(map[string]any)
	style, ok := tr["text_element_style"].(map[string]any)
	if !ok {
		t.Fatalf("missing text_element_style")
	}
	got := style[key]
	if got != want {
		t.Errorf("style[%q] = %v, want %v", key, got, want)
	}
}

// --- blocksToMarkdown tests ---

func TestBlocksToMarkdown(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		raw      any
		imageDir string
		want     string
	}{
		{
			name: "empty input returns empty",
			raw:  map[string]any{"items": []any{}},
			want: "",
		},
		{
			name: "invalid input returns empty",
			raw:  "not json",
			want: "",
		},
		{
			name: "page with title and text block",
			raw: map[string]any{
				"items": []any{
					map[string]any{
						"block_id":   "page1",
						"block_type": btPage,
						"children":   []any{"text1"},
						"text":       map[string]any{"elements": []any{map[string]any{"text_run": map[string]any{"content": "Doc Title"}}}},
					},
					map[string]any{
						"block_id":   "text1",
						"block_type": btText,
						"parent_id":  "page1",
						"text":       map[string]any{"elements": []any{map[string]any{"text_run": map[string]any{"content": "Hello world"}}}},
					},
				},
			},
			want: "# Doc Title\n\n<!-- bid:text1 -->\nHello world\n\n",
		},
		{
			name: "heading levels",
			raw: map[string]any{
				"items": []any{
					map[string]any{
						"block_id":   "page1",
						"block_type": btPage,
						"children":   []any{"h1", "h2", "h3"},
					},
					map[string]any{
						"block_id":   "h1",
						"block_type": btHeading1,
						"parent_id":  "page1",
						"heading1":   map[string]any{"elements": []any{map[string]any{"text_run": map[string]any{"content": "H1"}}}},
					},
					map[string]any{
						"block_id":   "h2",
						"block_type": btHeading2,
						"parent_id":  "page1",
						"heading2":   map[string]any{"elements": []any{map[string]any{"text_run": map[string]any{"content": "H2"}}}},
					},
					map[string]any{
						"block_id":   "h3",
						"block_type": btHeading3,
						"parent_id":  "page1",
						"heading3":   map[string]any{"elements": []any{map[string]any{"text_run": map[string]any{"content": "H3"}}}},
					},
				},
			},
			want: "<!-- bid:h1 -->\n# H1\n\n<!-- bid:h2 -->\n## H2\n\n<!-- bid:h3 -->\n### H3\n\n",
		},
		{
			name: "bullet list",
			raw: map[string]any{
				"items": []any{
					map[string]any{"block_id": "page1", "block_type": btPage, "children": []any{"b1"}},
					map[string]any{
						"block_id":   "b1",
						"block_type": btBullet,
						"parent_id":  "page1",
						"bullet":     map[string]any{"elements": []any{map[string]any{"text_run": map[string]any{"content": "Item 1"}}}},
					},
				},
			},
			want: "<!-- bid:b1 -->\n- Item 1\n",
		},
		{
			name: "ordered list with sequence",
			raw: map[string]any{
				"items": []any{
					map[string]any{"block_id": "page1", "block_type": btPage, "children": []any{"o1"}},
					map[string]any{
						"block_id":   "o1",
						"block_type": btOrdered,
						"parent_id":  "page1",
						"ordered":    map[string]any{"elements": []any{map[string]any{"text_run": map[string]any{"content": "First"}}}, "style": map[string]any{"sequence": "3"}},
					},
				},
			},
			want: "<!-- bid:o1 -->\n3. First\n",
		},
		{
			name: "ordered list with auto sequence defaults to 1",
			raw: map[string]any{
				"items": []any{
					map[string]any{"block_id": "page1", "block_type": btPage, "children": []any{"o1"}},
					map[string]any{
						"block_id":   "o1",
						"block_type": btOrdered,
						"parent_id":  "page1",
						"ordered":    map[string]any{"elements": []any{map[string]any{"text_run": map[string]any{"content": "Auto"}}}, "style": map[string]any{"sequence": "auto"}},
					},
				},
			},
			want: "<!-- bid:o1 -->\n1. Auto\n",
		},
		{
			name: "todo unchecked and checked",
			raw: map[string]any{
				"items": []any{
					map[string]any{"block_id": "page1", "block_type": btPage, "children": []any{"t1", "t2"}},
					map[string]any{
						"block_id":   "t1",
						"block_type": btTodo,
						"parent_id":  "page1",
						"todo":       map[string]any{"elements": []any{map[string]any{"text_run": map[string]any{"content": "Undone"}}}, "style": map[string]any{"done": false}},
					},
					map[string]any{
						"block_id":   "t2",
						"block_type": btTodo,
						"parent_id":  "page1",
						"todo":       map[string]any{"elements": []any{map[string]any{"text_run": map[string]any{"content": "Done"}}}, "style": map[string]any{"done": true}},
					},
				},
			},
			want: "<!-- bid:t1 -->\n- [ ] Undone\n<!-- bid:t2 -->\n- [x] Done\n",
		},
		{
			name: "quote block",
			raw: map[string]any{
				"items": []any{
					map[string]any{"block_id": "page1", "block_type": btPage, "children": []any{"q1"}},
					map[string]any{
						"block_id":   "q1",
						"block_type": btQuote,
						"parent_id":  "page1",
						"quote":      map[string]any{"elements": []any{map[string]any{"text_run": map[string]any{"content": "Quoted text"}}}},
					},
				},
			},
			want: "<!-- bid:q1 -->\n> Quoted text\n\n",
		},
		{
			name: "code block with language",
			raw: map[string]any{
				"items": []any{
					map[string]any{"block_id": "page1", "block_type": btPage, "children": []any{"c1"}},
					map[string]any{
						"block_id":   "c1",
						"block_type": btCode,
						"parent_id":  "page1",
						"code": map[string]any{
							"elements": []any{map[string]any{"text_run": map[string]any{"content": "fmt.Println(\"hi\")"}}},
							"style":    map[string]any{"language": float64(22)},
						},
					},
				},
			},
			want: "<!-- bid:c1 -->\n```go\nfmt.Println(\"hi\")\n```\n\n",
		},
		{
			name: "divider block",
			raw: map[string]any{
				"items": []any{
					map[string]any{"block_id": "page1", "block_type": btPage, "children": []any{"d1"}},
					map[string]any{"block_id": "d1", "block_type": btDivider, "parent_id": "page1"},
				},
			},
			want: "<!-- bid:d1 -->\n---\n\n",
		},
		{
			name: "image with imageDir",
			raw: map[string]any{
				"items": []any{
					map[string]any{"block_id": "page1", "block_type": btPage, "children": []any{"i1"}},
					map[string]any{
						"block_id":   "i1",
						"block_type": btImage,
						"parent_id":  "page1",
						"image":      map[string]any{"token": "img-abc123"},
					},
				},
			},
			imageDir: "/tmp/images",
			want:     "<!-- bid:i1 -->\n![image](/tmp/images/img-abc123)\n\n",
		},
		{
			name: "image without imageDir",
			raw: map[string]any{
				"items": []any{
					map[string]any{"block_id": "page1", "block_type": btPage, "children": []any{"i1"}},
					map[string]any{
						"block_id":   "i1",
						"block_type": btImage,
						"parent_id":  "page1",
						"image":      map[string]any{"token": "img-abc123"},
					},
				},
			},
			imageDir: "",
			want:     "<!-- bid:i1 -->\n![image](feishu-media://img-abc123)\n\n",
		},
		{
			name: "file block",
			raw: map[string]any{
				"items": []any{
					map[string]any{"block_id": "page1", "block_type": btPage, "children": []any{"f1"}},
					map[string]any{
						"block_id":   "f1",
						"block_type": btFile,
						"parent_id":  "page1",
						"file":       map[string]any{"token": "file-tok", "name": "readme.pdf"},
					},
				},
			},
			want: "<!-- bid:f1 -->\n[readme.pdf](feishu-file://file-tok)\n\n",
		},
		{
			name: "nested bullets",
			raw: map[string]any{
				"items": []any{
					map[string]any{"block_id": "page1", "block_type": btPage, "children": []any{"b1"}},
					map[string]any{
						"block_id":   "b1",
						"block_type": btBullet,
						"parent_id":  "page1",
						"children":   []any{"b2"},
						"bullet":     map[string]any{"elements": []any{map[string]any{"text_run": map[string]any{"content": "Parent"}}}},
					},
					map[string]any{
						"block_id":   "b2",
						"block_type": btBullet,
						"parent_id":  "b1",
						"bullet":     map[string]any{"elements": []any{map[string]any{"text_run": map[string]any{"content": "Child"}}}},
					},
				},
			},
			want: "<!-- bid:b1 -->\n- Parent\n  - Child\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := blocksToMarkdown(tt.raw, tt.imageDir)
			if got != tt.want {
				t.Errorf("blocksToMarkdown() =\n%q\nwant:\n%q", got, tt.want)
			}
		})
	}
}

func TestBlocksToMarkdownPageTitle(t *testing.T) {
	t.Parallel()

	// Test page block with heading1 as title (not text)
	raw := map[string]any{
		"items": []any{
			map[string]any{
				"block_id":   "page1",
				"block_type": btPage,
				"children":   []any{},
				"heading1":   map[string]any{"elements": []any{map[string]any{"text_run": map[string]any{"content": "Title via H1"}}}},
			},
		},
	}
	got := blocksToMarkdown(raw, "")
	if !strings.HasPrefix(got, "# Title via H1\n") {
		t.Errorf("expected title from heading1, got %q", got)
	}
}

func TestBlocksToMarkdownPageNoTitle(t *testing.T) {
	t.Parallel()

	// Test page block with no Text and no Heading1 - exercises the else branch
	// The raw extraction for "page" field requires it to survive JSON round-trip,
	// but since blockData doesn't have a Page field, the page data is lost.
	// This test verifies the code handles that gracefully (no title output).
	raw := map[string]any{
		"items": []any{
			map[string]any{
				"block_id":   "page1",
				"block_type": btPage,
				"children":   []any{"t1"},
			},
			map[string]any{
				"block_id":   "t1",
				"block_type": btText,
				"parent_id":  "page1",
				"text":       map[string]any{"elements": []any{map[string]any{"text_run": map[string]any{"content": "body"}}}},
			},
		},
	}

	got := blocksToMarkdown(raw, "")
	// Should not start with "# " since there's no title
	if strings.HasPrefix(got, "# ") {
		t.Errorf("expected no title, got %q", got)
	}
	if !strings.Contains(got, "body") {
		t.Errorf("expected body content, got %q", got)
	}
}

// --- renderTable tests ---

func TestRenderTable(t *testing.T) {
	t.Parallel()

	blockMap := map[string]*blockData{
		"table1": {
			BlockID:   "table1",
			BlockType: btTable,
			Table: &tableBlock{
				Cells:    []string{"c1", "c2", "c3", "c4"},
				Property: tableProperty{ColumnSize: 2, RowCount: 2},
			},
		},
		"c1": {
			BlockID:   "c1",
			BlockType: btTableCell,
			Children:  []string{"c1t"},
		},
		"c1t": {
			BlockID:   "c1t",
			BlockType: btText,
			Text:      &richText{Elements: []element{{TextRun: &textRun{Content: "Header A"}}}},
		},
		"c2": {
			BlockID:   "c2",
			BlockType: btTableCell,
			Children:  []string{"c2t"},
		},
		"c2t": {
			BlockID:   "c2t",
			BlockType: btText,
			Text:      &richText{Elements: []element{{TextRun: &textRun{Content: "Header B"}}}},
		},
		"c3": {
			BlockID:   "c3",
			BlockType: btTableCell,
			Children:  []string{"c3t"},
		},
		"c3t": {
			BlockID:   "c3t",
			BlockType: btText,
			Text:      &richText{Elements: []element{{TextRun: &textRun{Content: "Val 1"}}}},
		},
		"c4": {
			BlockID:   "c4",
			BlockType: btTableCell,
			Children:  []string{"c4t"},
		},
		"c4t": {
			BlockID:   "c4t",
			BlockType: btText,
			Text:      &richText{Elements: []element{{TextRun: &textRun{Content: "Val 2"}}}},
		},
	}

	var sb strings.Builder
	renderTable(&sb, blockMap["table1"], blockMap)
	got := sb.String()

	if !strings.Contains(got, "| Header A | Header B |") {
		t.Errorf("missing header row, got:\n%s", got)
	}
	if !strings.Contains(got, "| --- | --- |") {
		t.Errorf("missing separator row, got:\n%s", got)
	}
	if !strings.Contains(got, "| Val 1 | Val 2 |") {
		t.Errorf("missing data row, got:\n%s", got)
	}
}

func TestRenderTableNilTable(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	renderTable(&sb, &blockData{BlockType: btTable}, nil)
	if sb.String() != "" {
		t.Errorf("expected empty output for nil table, got %q", sb.String())
	}
}

func TestRenderTableZeroCols(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	renderTable(&sb, &blockData{BlockType: btTable, Table: &tableBlock{Property: tableProperty{ColumnSize: 0}}}, nil)
	if sb.String() != "" {
		t.Errorf("expected empty output for zero cols, got %q", sb.String())
	}
}

// --- renderCellContent tests ---

func TestRenderCellContent(t *testing.T) {
	t.Parallel()

	blockMap := map[string]*blockData{
		"cell1": {
			BlockID:   "cell1",
			BlockType: btTableCell,
			Children:  []string{"t1", "t2"},
		},
		"t1": {
			BlockID:   "t1",
			BlockType: btText,
			Text:      &richText{Elements: []element{{TextRun: &textRun{Content: "part1"}}}},
		},
		"t2": {
			BlockID:   "t2",
			BlockType: btText,
			Text:      &richText{Elements: []element{{TextRun: &textRun{Content: "part2"}}}},
		},
	}

	got := renderCellContent(blockMap["cell1"], blockMap)
	if got != "part1 part2" {
		t.Errorf("renderCellContent() = %q, want %q", got, "part1 part2")
	}
}

func TestRenderCellContentMissingChild(t *testing.T) {
	t.Parallel()
	blockMap := map[string]*blockData{
		"cell1": {
			BlockID:   "cell1",
			BlockType: btTableCell,
			Children:  []string{"nonexistent"},
		},
	}
	got := renderCellContent(blockMap["cell1"], blockMap)
	if got != "" {
		t.Errorf("renderCellContent() = %q, want empty", got)
	}
}

// --- getHeadingRT tests ---

func TestGetHeadingRT(t *testing.T) {
	t.Parallel()

	rt := &richText{Elements: []element{{TextRun: &textRun{Content: "heading"}}}}

	tests := []struct {
		name    string
		block   *blockData
		wantNil bool
	}{
		{"heading1", &blockData{BlockType: btHeading1, Heading1: rt}, false},
		{"heading2", &blockData{BlockType: btHeading2, Heading2: rt}, false},
		{"heading3", &blockData{BlockType: btHeading3, Heading3: rt}, false},
		{"heading4", &blockData{BlockType: btHeading4, Heading4: rt}, false},
		{"heading5", &blockData{BlockType: btHeading5, Heading5: rt}, false},
		{"heading6", &blockData{BlockType: btHeading6, Heading6: rt}, false},
		{"heading7", &blockData{BlockType: btHeading7, Heading7: rt}, false},
		{"heading8", &blockData{BlockType: btHeading8, Heading8: rt}, false},
		{"heading9", &blockData{BlockType: btHeading9, Heading9: rt}, false},
		{"text returns nil", &blockData{BlockType: btText}, true},
		{"bullet returns nil", &blockData{BlockType: btBullet}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getHeadingRT(tt.block)
			if tt.wantNil && got != nil {
				t.Errorf("expected nil, got %+v", got)
			}
			if !tt.wantNil && got == nil {
				t.Errorf("expected non-nil richText")
			}
		})
	}
}

// --- getContentRT tests ---

func TestGetContentRT(t *testing.T) {
	t.Parallel()

	rt := &richText{Elements: []element{{TextRun: &textRun{Content: "content"}}}}

	tests := []struct {
		name    string
		block   *blockData
		wantNil bool
	}{
		{"text", &blockData{BlockType: btText, Text: rt}, false},
		{"bullet", &blockData{BlockType: btBullet, Bullet: rt}, false},
		{"ordered", &blockData{BlockType: btOrdered, Ordered: rt}, false},
		{"quote", &blockData{BlockType: btQuote, Quote: rt}, false},
		{"todo", &blockData{BlockType: btTodo, Todo: rt}, false},
		{"heading via fallback", &blockData{BlockType: btHeading4, Heading4: rt}, false},
		{"code returns nil", &blockData{BlockType: btCode}, true},
		{"divider returns nil", &blockData{BlockType: btDivider}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getContentRT(tt.block)
			if tt.wantNil && got != nil {
				t.Errorf("expected nil")
			}
			if !tt.wantNil && got == nil {
				t.Errorf("expected non-nil")
			}
		})
	}
}

// --- headingBlockType tests ---

func TestHeadingBlockType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		level int
		want  int
	}{
		{1, btHeading1}, {2, btHeading2}, {3, btHeading3}, {4, btHeading4},
		{5, btHeading5}, {6, btHeading6}, {7, btHeading7}, {8, btHeading8},
		{9, btHeading9}, {0, btHeading4}, {10, btHeading4}, {-1, btHeading4},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("level_%d", tt.level), func(t *testing.T) {
			got := headingBlockType(tt.level)
			if got != tt.want {
				t.Errorf("headingBlockType(%d) = %d, want %d", tt.level, got, tt.want)
			}
		})
	}
}

// --- headingFieldName tests ---

func TestHeadingFieldName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		blockType int
		want      string
	}{
		{btHeading1, "heading1"}, {btHeading2, "heading2"}, {btHeading3, "heading3"},
		{btHeading4, "heading4"}, {btHeading5, "heading5"}, {btHeading6, "heading6"},
		{btHeading7, "heading7"}, {btHeading8, "heading8"}, {btHeading9, "heading9"},
		{btText, "heading4"}, {0, "heading4"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := headingFieldName(tt.blockType)
			if got != tt.want {
				t.Errorf("headingFieldName(%d) = %q, want %q", tt.blockType, got, tt.want)
			}
		})
	}
}

// --- isHeadingBlockType tests ---

func TestIsHeadingBlockType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		blockType int
		want      bool
	}{
		{btHeading1, true}, {btHeading9, true}, {btHeading5, true},
		{btText, false}, {btBullet, false}, {btCode, false},
		{btHeading1 - 1, false}, {btHeading9 + 1, false},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("type_%d", tt.blockType), func(t *testing.T) {
			got := isHeadingBlockType(tt.blockType)
			if got != tt.want {
				t.Errorf("isHeadingBlockType(%d) = %v, want %v", tt.blockType, got, tt.want)
			}
		})
	}
}

// --- parseMarkdownOrderedItem tests ---

func TestParseMarkdownOrderedItem(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		line    string
		wantSeq string
		wantTxt string
		wantOK  bool
	}{
		{"basic", "1. First item", "1", "First item", true},
		{"multi-digit", "42. Forty two", "42", "Forty two", true},
		{"no dot space", "1.NoSpace", "", "", false},
		{"no number", "abc. text", "", "", false},
		{"empty text", "1. ", "", "", false},
		{"just dot", ". text", "", "", false},
		{"negative", "-1. text", "", "", false},
		{"plain text", "just text", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seq, txt, ok := parseMarkdownOrderedItem(tt.line)
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
			if seq != tt.wantSeq {
				t.Errorf("seq = %q, want %q", seq, tt.wantSeq)
			}
			if txt != tt.wantTxt {
				t.Errorf("txt = %q, want %q", txt, tt.wantTxt)
			}
		})
	}
}

// --- parseMarkdownImageToken tests ---

func TestParseMarkdownImageToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		line   string
		want   string
		wantOK bool
	}{
		{"feishu-media", "![image](feishu-media://img-token)", "img-token", true},
		{"url image", "![alt](https://example.com/images/photo.jpg)", "photo.jpg", true},
		{"not image", "just text", "", false},
		{"no prefix", "[image](url)", "", false},
		{"empty ref", "![image]()", "", false},
		{"missing closing paren", "![image](feishu-media://tok", "", false},
		{"local path", "![image](/tmp/images/img-abc)", "img-abc", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, ok := parseMarkdownImageToken(tt.line)
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
			if token != tt.want {
				t.Errorf("token = %q, want %q", token, tt.want)
			}
		})
	}
}

// --- parseMarkdownFileLink tests ---

func TestParseMarkdownFileLink(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		line      string
		wantName  string
		wantToken string
		wantOK    bool
	}{
		{"valid", "[spec.pdf](feishu-file://file-tok)", "spec.pdf", "file-tok", true},
		{"not file link", "[text](https://example.com)", "", "", false},
		{"no prefix bracket", "text", "", "", false},
		{"empty name", "[](feishu-file://tok)", "", "", false},
		{"empty token", "[name](feishu-file://)", "", "", false},
		{"missing close paren", "[name](feishu-file://tok", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, token, ok := parseMarkdownFileLink(tt.line)
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
			if name != tt.wantName {
				t.Errorf("name = %q, want %q", name, tt.wantName)
			}
			if token != tt.wantToken {
				t.Errorf("token = %q, want %q", token, tt.wantToken)
			}
		})
	}
}

// --- isListLine tests ---

func TestIsListLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		line string
		want bool
	}{
		{"- top level", true},
		{"  - nested bullet", true},
		{"    - deep nested", true},
		{"  - [ ] nested todo", true},
		{"  1. nested ordered", true},
		{"1. ordered", true},
		{"no indent", false},
		{"  plain text", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			got := isListLine(tt.line)
			if got != tt.want {
				t.Errorf("isListLine(%q) = %v, want %v", tt.line, got, tt.want)
			}
		})
	}
}

// --- markdownToBlocksWithIDs tests ---

func TestMarkdownToBlocksWithIDs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		md         string
		wantLen    int
		wantErr    bool
		errContain string
		check      func(t *testing.T, entries []mdBlockEntry)
	}{
		{
			name:    "empty markdown",
			md:      "",
			wantLen: 0,
		},
		{
			name:    "plain text",
			md:      "Hello world",
			wantLen: 1,
			check: func(t *testing.T, entries []mdBlockEntry) {
				if entries[0].BlockType != btText {
					t.Errorf("block type = %d, want text", entries[0].BlockType)
				}
				if entries[0].Comparable != "Hello world" {
					t.Errorf("comparable = %q", entries[0].Comparable)
				}
			},
		},
		{
			name:    "heading without bid uses real level",
			md:      "## My Heading",
			wantLen: 1,
			check: func(t *testing.T, entries []mdBlockEntry) {
				if entries[0].BlockType != btHeading2 {
					t.Errorf("block type = %d, want heading2 (%d)", entries[0].BlockType, btHeading2)
				}
			},
		},
		{
			name:    "heading with bid uses correct level",
			md:      "<!-- bid:h1 -->\n## My Heading",
			wantLen: 1,
			check: func(t *testing.T, entries []mdBlockEntry) {
				if entries[0].BlockType != btHeading2 {
					t.Errorf("block type = %d, want heading2 (%d)", entries[0].BlockType, btHeading2)
				}
				if entries[0].BlockID != "h1" {
					t.Errorf("block id = %q, want h1", entries[0].BlockID)
				}
			},
		},
		{
			name:    "code block go",
			md:      "```go\nfmt.Println(\"hello\")\n```",
			wantLen: 1,
			check: func(t *testing.T, entries []mdBlockEntry) {
				if entries[0].BlockType != btCode {
					t.Errorf("block type = %d, want code", entries[0].BlockType)
				}
				if !strings.Contains(entries[0].Comparable, "fmt.Println") {
					t.Errorf("comparable = %q", entries[0].Comparable)
				}
			},
		},
		{
			name:    "code block with alias",
			md:      "```py\nprint('hi')\n```",
			wantLen: 1,
			check: func(t *testing.T, entries []mdBlockEntry) {
				if !strings.HasPrefix(entries[0].Comparable, "47\n") {
					t.Errorf("comparable = %q, want python lang code 47", entries[0].Comparable)
				}
			},
		},
		{
			name:    "code block unknown language",
			md:      "```unknownlang\ncode here\n```",
			wantLen: 1,
			check: func(t *testing.T, entries []mdBlockEntry) {
				if !strings.HasPrefix(entries[0].Comparable, "1\n") {
					t.Errorf("comparable = %q, want default lang code 1", entries[0].Comparable)
				}
			},
		},
		{
			name:    "empty code block skipped",
			md:      "```\n```",
			wantLen: 0,
		},
		{
			name:    "divider",
			md:      "---",
			wantLen: 1,
			check: func(t *testing.T, entries []mdBlockEntry) {
				if entries[0].BlockType != btDivider {
					t.Errorf("block type = %d, want divider", entries[0].BlockType)
				}
				if entries[0].Comparable != "divider" {
					t.Errorf("comparable = %q", entries[0].Comparable)
				}
			},
		},
		{
			name:    "bullet without bid creates bullet block",
			md:      "- Item one",
			wantLen: 1,
			check: func(t *testing.T, entries []mdBlockEntry) {
				if entries[0].BlockType != btBullet {
					t.Errorf("block type = %d, want bullet", entries[0].BlockType)
				}
				if entries[0].Comparable != "Item one" {
					t.Errorf("comparable = %q", entries[0].Comparable)
				}
			},
		},
		{
			name:    "bullet with bid creates bullet",
			md:      "<!-- bid:b1 -->\n- Item one",
			wantLen: 1,
			check: func(t *testing.T, entries []mdBlockEntry) {
				if entries[0].BlockType != btBullet {
					t.Errorf("block type = %d, want bullet", entries[0].BlockType)
				}
				if entries[0].BlockID != "b1" {
					t.Errorf("block id = %q", entries[0].BlockID)
				}
			},
		},
		{
			name:    "todo without bid creates todo block",
			md:      "- [ ] task",
			wantLen: 1,
			check: func(t *testing.T, entries []mdBlockEntry) {
				if entries[0].BlockType != btTodo {
					t.Errorf("block type = %d, want todo", entries[0].BlockType)
				}
			},
		},
		{
			name:    "todo with bid",
			md:      "<!-- bid:t1 -->\n- [ ] My task",
			wantLen: 1,
			check: func(t *testing.T, entries []mdBlockEntry) {
				if entries[0].BlockType != btTodo {
					t.Errorf("block type = %d, want todo", entries[0].BlockType)
				}
				if entries[0].BlockID != "t1" {
					t.Errorf("block id = %q, want t1", entries[0].BlockID)
				}
			},
		},
		{
			name:    "checked todo",
			md:      "<!-- bid:t1 -->\n- [x] Done task",
			wantLen: 1,
			check: func(t *testing.T, entries []mdBlockEntry) {
				if entries[0].BlockType != btTodo {
					t.Errorf("block type = %d, want todo", entries[0].BlockType)
				}
			},
		},
		{
			name:    "ordered without bid creates ordered block",
			md:      "1. first",
			wantLen: 1,
			check: func(t *testing.T, entries []mdBlockEntry) {
				if entries[0].BlockType != btOrdered {
					t.Errorf("block type = %d, want ordered", entries[0].BlockType)
				}
				if entries[0].Comparable != "1. first" {
					t.Errorf("comparable = %q", entries[0].Comparable)
				}
			},
		},
		{
			name:    "ordered with bid",
			md:      "<!-- bid:o1 -->\n1. first",
			wantLen: 1,
			check: func(t *testing.T, entries []mdBlockEntry) {
				if entries[0].BlockType != btOrdered {
					t.Errorf("block type = %d, want ordered", entries[0].BlockType)
				}
				if entries[0].Comparable != "1. first" {
					t.Errorf("comparable = %q", entries[0].Comparable)
				}
			},
		},
		{
			name:    "quote without bid creates quote_container",
			md:      "> quoted text",
			wantLen: 1,
			check: func(t *testing.T, entries []mdBlockEntry) {
				if entries[0].BlockType != btQuoteContainer {
					t.Errorf("block type = %d, want quote_container (%d)", entries[0].BlockType, btQuoteContainer)
				}
				if len(entries[0].Children) != 1 {
					t.Fatalf("children = %d, want 1", len(entries[0].Children))
				}
				if entries[0].Children[0].Comparable != "quoted text" {
					t.Errorf("child comparable = %q", entries[0].Children[0].Comparable)
				}
			},
		},
		{
			name:    "multi-line quote",
			md:      "> line one\n> line two",
			wantLen: 1,
			check: func(t *testing.T, entries []mdBlockEntry) {
				if entries[0].BlockType != btQuoteContainer {
					t.Errorf("block type = %d, want quote_container", entries[0].BlockType)
				}
				if len(entries[0].Children) != 2 {
					t.Fatalf("children = %d, want 2", len(entries[0].Children))
				}
			},
		},
		{
			name:    "nested quote flattened",
			md:      "> outer\n> > inner",
			wantLen: 1,
			check: func(t *testing.T, entries []mdBlockEntry) {
				if len(entries[0].Children) != 2 {
					t.Fatalf("children = %d, want 2", len(entries[0].Children))
				}
				if entries[0].Children[1].Comparable != "inner" {
					t.Errorf("nested quote text = %q, want %q", entries[0].Children[1].Comparable, "inner")
				}
			},
		},
		{
			name:    "quote with bid creates quote_container",
			md:      "<!-- bid:q1 -->\n> quoted text",
			wantLen: 1,
			check: func(t *testing.T, entries []mdBlockEntry) {
				if entries[0].BlockType != btQuoteContainer {
					t.Errorf("block type = %d, want quote_container", entries[0].BlockType)
				}
				if entries[0].BlockID != "q1" {
					t.Errorf("block id = %q, want q1", entries[0].BlockID)
				}
			},
		},
		{
			name:       "table without bid errors",
			md:         "| A | B |\n| --- | --- |\n| 1 | 2 |",
			wantErr:    true,
			errContain: "creating or moving tables",
		},
		{
			name:       "image without bid errors",
			md:         "![img](feishu-media://tok)",
			wantErr:    true,
			errContain: "creating or moving images",
		},
		{
			name:       "file link without bid errors",
			md:         "[f.pdf](feishu-file://tok)",
			wantErr:    true,
			errContain: "creating or moving file links",
		},
		{
			name:    "multiple blocks",
			md:      "<!-- bid:h1 -->\n# Title\n\n<!-- bid:t1 -->\nSome text\n\n<!-- bid:d1 -->\n---",
			wantLen: 3,
			check: func(t *testing.T, entries []mdBlockEntry) {
				if entries[0].BlockType != btHeading1 {
					t.Errorf("first block type = %d, want heading1", entries[0].BlockType)
				}
				if entries[1].BlockType != btText {
					t.Errorf("second block type = %d, want text", entries[1].BlockType)
				}
				if entries[2].BlockType != btDivider {
					t.Errorf("third block type = %d, want divider", entries[2].BlockType)
				}
			},
		},
		{
			name:    "source line tracking",
			md:      "Line one\n\nLine three",
			wantLen: 2,
			check: func(t *testing.T, entries []mdBlockEntry) {
				if entries[0].SourceLine != 1 {
					t.Errorf("first source line = %d, want 1", entries[0].SourceLine)
				}
				if entries[1].SourceLine != 3 {
					t.Errorf("second source line = %d, want 3", entries[1].SourceLine)
				}
			},
		},
		{
			name:    "heading levels 1-9 with bid",
			md:      "<!-- bid:h -->\n######### Level 9",
			wantLen: 1,
			check: func(t *testing.T, entries []mdBlockEntry) {
				if entries[0].BlockType != btHeading9 {
					t.Errorf("block type = %d, want heading9 (%d)", entries[0].BlockType, btHeading9)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entries, err := markdownToBlocksWithIDs(tt.md)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errContain != "" && !strings.Contains(err.Error(), tt.errContain) {
					t.Errorf("error = %q, want to contain %q", err.Error(), tt.errContain)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(entries) != tt.wantLen {
				t.Fatalf("len = %d, want %d", len(entries), tt.wantLen)
			}
			if tt.check != nil {
				tt.check(t, entries)
			}
		})
	}
}

// --- blockComparableContent tests ---

func TestBlockComparableContent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		block *blockData
		want  string
	}{
		{
			name: "text block",
			block: &blockData{
				BlockType: btText,
				Text:      &richText{Elements: []element{{TextRun: &textRun{Content: "hello"}}}},
			},
			want: "hello",
		},
		{
			name:  "text block nil",
			block: &blockData{BlockType: btText},
			want:  "",
		},
		{
			name: "bullet block",
			block: &blockData{
				BlockType: btBullet,
				Bullet:    &richText{Elements: []element{{TextRun: &textRun{Content: "item"}}}},
			},
			want: "item",
		},
		{
			name:  "bullet block nil",
			block: &blockData{BlockType: btBullet},
			want:  "",
		},
		{
			name: "ordered block with sequence",
			block: &blockData{
				BlockType: btOrdered,
				Ordered: &richText{
					Elements: []element{{TextRun: &textRun{Content: "first"}}},
					Style:    blockStyle{Sequence: "3"},
				},
			},
			want: "3. first",
		},
		{
			name: "ordered block auto sequence",
			block: &blockData{
				BlockType: btOrdered,
				Ordered: &richText{
					Elements: []element{{TextRun: &textRun{Content: "auto"}}},
					Style:    blockStyle{Sequence: "auto"},
				},
			},
			want: "1. auto",
		},
		{
			name: "ordered block empty sequence",
			block: &blockData{
				BlockType: btOrdered,
				Ordered: &richText{
					Elements: []element{{TextRun: &textRun{Content: "empty"}}},
					Style:    blockStyle{Sequence: ""},
				},
			},
			want: "1. empty",
		},
		{
			name: "ordered block undefined sequence",
			block: &blockData{
				BlockType: btOrdered,
				Ordered: &richText{
					Elements: []element{{TextRun: &textRun{Content: "undef"}}},
					Style:    blockStyle{Sequence: "undefined"},
				},
			},
			want: "1. undef",
		},
		{
			name:  "ordered block nil",
			block: &blockData{BlockType: btOrdered},
			want:  "",
		},
		{
			name: "todo unchecked",
			block: &blockData{
				BlockType: btTodo,
				Todo: &richText{
					Elements: []element{{TextRun: &textRun{Content: "task"}}},
					Style:    blockStyle{Done: false},
				},
			},
			want: "[ ] task",
		},
		{
			name: "todo checked",
			block: &blockData{
				BlockType: btTodo,
				Todo: &richText{
					Elements: []element{{TextRun: &textRun{Content: "done"}}},
					Style:    blockStyle{Done: true},
				},
			},
			want: "[x] done",
		},
		{
			name:  "todo nil",
			block: &blockData{BlockType: btTodo},
			want:  "",
		},
		{
			name: "quote block",
			block: &blockData{
				BlockType: btQuote,
				Quote:     &richText{Elements: []element{{TextRun: &textRun{Content: "quoted"}}}},
			},
			want: "quoted",
		},
		{
			name:  "quote nil",
			block: &blockData{BlockType: btQuote},
			want:  "",
		},
		{
			name: "code block",
			block: &blockData{
				BlockType: btCode,
				Code: &codeBlock{
					Elements: []element{{TextRun: &textRun{Content: "x := 1"}}},
					Style:    codeStyle{Language: 22},
				},
			},
			want: "22\nx := 1",
		},
		{
			name:  "code nil",
			block: &blockData{BlockType: btCode},
			want:  "",
		},
		{
			name:  "divider",
			block: &blockData{BlockType: btDivider},
			want:  "divider",
		},
		{
			name: "image",
			block: &blockData{
				BlockType: btImage,
				Image:     &imageBlock{Token: "tok123"},
			},
			want: "image:tok123",
		},
		{
			name:  "image nil",
			block: &blockData{BlockType: btImage},
			want:  "",
		},
		{
			name: "file",
			block: &blockData{
				BlockType: btFile,
				File:      &fileBlock{Token: "ftok", Name: "readme.md"},
			},
			want: "file:readme.md\tftok",
		},
		{
			name:  "file nil",
			block: &blockData{BlockType: btFile},
			want:  "",
		},
		{
			name: "heading via default",
			block: &blockData{
				BlockType: btHeading3,
				Heading3:  &richText{Elements: []element{{TextRun: &textRun{Content: "h3 text"}}}},
			},
			want: "h3 text",
		},
		{
			name:  "unknown type no heading",
			block: &blockData{BlockType: 999},
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := blockComparableContent(tt.block, nil)
			if got != tt.want {
				t.Errorf("blockComparableContent() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBlockComparableContentTable(t *testing.T) {
	t.Parallel()

	blockMap := map[string]*blockData{
		"table": {
			BlockID:   "table",
			BlockType: btTable,
			Table: &tableBlock{
				Cells:    []string{"c1", "c2"},
				Property: tableProperty{ColumnSize: 2, RowCount: 1},
			},
		},
		"c1": {BlockID: "c1", BlockType: btTableCell, Children: []string{"t1"}},
		"c2": {BlockID: "c2", BlockType: btTableCell, Children: []string{"t2"}},
		"t1": {BlockID: "t1", BlockType: btText, Text: &richText{Elements: []element{{TextRun: &textRun{Content: "A"}}}}},
		"t2": {BlockID: "t2", BlockType: btText, Text: &richText{Elements: []element{{TextRun: &textRun{Content: "B"}}}}},
	}

	got := blockComparableContent(blockMap["table"], blockMap)
	if !strings.Contains(got, "A") && !strings.Contains(got, "B") {
		t.Errorf("table comparable = %q", got)
	}
}

// --- buildUpdateRequest tests ---

func TestBuildUpdateRequest(t *testing.T) {
	t.Parallel()

	t.Run("with text elements", func(t *testing.T) {
		entry := mdBlockEntry{
			BlockType: btText,
			Body: map[string]any{
				"block_type": btText,
				"text": map[string]any{
					"elements": []any{map[string]any{"text_run": map[string]any{"content": "hello"}}},
				},
			},
		}
		req := buildUpdateRequest("block1", entry)
		if req["block_id"] != "block1" {
			t.Errorf("block_id = %v", req["block_id"])
		}
		if req["update_text_elements"] == nil {
			t.Error("missing update_text_elements")
		}
	})

	t.Run("nil body", func(t *testing.T) {
		entry := mdBlockEntry{BlockType: btText}
		req := buildUpdateRequest("block1", entry)
		if req["block_id"] != "block1" {
			t.Errorf("block_id = %v", req["block_id"])
		}
		if _, ok := req["update_text_elements"]; ok {
			t.Error("should not have update_text_elements for nil body")
		}
	})
}

// --- entryElements tests ---

func TestEntryElements(t *testing.T) {
	t.Parallel()

	elems := []any{map[string]any{"text_run": map[string]any{"content": "x"}}}

	tests := []struct {
		name    string
		entry   mdBlockEntry
		wantNil bool
	}{
		{
			name:    "nil body",
			entry:   mdBlockEntry{BlockType: btText, Body: nil},
			wantNil: true,
		},
		{
			name: "text",
			entry: mdBlockEntry{
				BlockType: btText,
				Body:      map[string]any{"text": map[string]any{"elements": elems}},
			},
			wantNil: false,
		},
		{
			name: "bullet",
			entry: mdBlockEntry{
				BlockType: btBullet,
				Body:      map[string]any{"bullet": map[string]any{"elements": elems}},
			},
			wantNil: false,
		},
		{
			name: "code",
			entry: mdBlockEntry{
				BlockType: btCode,
				Body:      map[string]any{"code": map[string]any{"elements": elems}},
			},
			wantNil: false,
		},
		{
			name: "heading4",
			entry: mdBlockEntry{
				BlockType: btHeading4,
				Body:      map[string]any{"heading4": map[string]any{"elements": elems}},
			},
			wantNil: false,
		},
		{
			name: "heading1",
			entry: mdBlockEntry{
				BlockType: btHeading1,
				Body:      map[string]any{"heading1": map[string]any{"elements": elems}},
			},
			wantNil: false,
		},
		{
			name: "text with wrong key",
			entry: mdBlockEntry{
				BlockType: btText,
				Body:      map[string]any{"wrong": map[string]any{"elements": elems}},
			},
			wantNil: true,
		},
		{
			name: "divider no elements",
			entry: mdBlockEntry{
				BlockType: btDivider,
				Body:      map[string]any{"block_type": btDivider},
			},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := entryElements(tt.entry)
			if tt.wantNil && got != nil {
				t.Errorf("expected nil, got %v", got)
			}
			if !tt.wantNil && got == nil {
				t.Error("expected non-nil")
			}
		})
	}
}

// --- blockTypeName tests ---

func TestBlockTypeName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		blockType int
		want      string
	}{
		{btText, "text"},
		{btHeading1, "heading1"},
		{btHeading2, "heading2"},
		{btHeading3, "heading3"},
		{btHeading4, "heading4"},
		{btHeading5, "heading5"},
		{btHeading6, "heading6"},
		{btHeading7, "heading7"},
		{btHeading8, "heading8"},
		{btHeading9, "heading9"},
		{btBullet, "bullet"},
		{btOrdered, "ordered"},
		{btCode, "code"},
		{btQuote, "quote"},
		{btQuoteContainer, "quote_container"},
		{btCallout, "callout"},
		{btTodo, "todo"},
		{btDivider, "divider"},
		{btFile, "file"},
		{btImage, "image"},
		{btTable, "table"},
		{999, "block_type_999"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := blockTypeName(tt.blockType)
			if got != tt.want {
				t.Errorf("blockTypeName(%d) = %q, want %q", tt.blockType, got, tt.want)
			}
		})
	}
}

// --- renderRawElements tests ---

func TestRenderRawElements(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		elems []any
		want  string
	}{
		{
			name:  "nil",
			elems: nil,
			want:  "",
		},
		{
			name: "single text_run",
			elems: []any{
				map[string]any{"text_run": map[string]any{"content": "hello"}},
			},
			want: "hello",
		},
		{
			name: "multiple text_runs",
			elems: []any{
				map[string]any{"text_run": map[string]any{"content": "hello "}},
				map[string]any{"text_run": map[string]any{"content": "world"}},
			},
			want: "hello world",
		},
		{
			name: "non-map element skipped",
			elems: []any{
				"not a map",
				map[string]any{"text_run": map[string]any{"content": "ok"}},
			},
			want: "ok",
		},
		{
			name: "missing text_run skipped",
			elems: []any{
				map[string]any{"other": "value"},
				map[string]any{"text_run": map[string]any{"content": "visible"}},
			},
			want: "visible",
		},
		{
			name: "missing content returns empty",
			elems: []any{
				map[string]any{"text_run": map[string]any{"style": "bold"}},
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderRawElements(tt.elems)
			if got != tt.want {
				t.Errorf("renderRawElements() = %q, want %q", got, tt.want)
			}
		})
	}
}

// --- renderBlock iframe and unknown type tests ---

func TestRenderBlockIframe(t *testing.T) {
	t.Parallel()

	blockMap := map[string]*blockData{
		"iframe1": {
			BlockID:   "iframe1",
			BlockType: btIframe,
			Children:  []string{"text1"},
		},
		"text1": {
			BlockID:   "text1",
			BlockType: btText,
			Text:      &richText{Elements: []element{{TextRun: &textRun{Content: "embedded"}}}},
		},
	}

	var sb strings.Builder
	renderBlock(&sb, blockMap["iframe1"], blockMap, "", 0)
	got := sb.String()
	if !strings.Contains(got, "embedded") {
		t.Errorf("iframe should render children, got %q", got)
	}
}

func TestRenderBlockUnknownTypeWithChildren(t *testing.T) {
	t.Parallel()

	blockMap := map[string]*blockData{
		"unknown1": {
			BlockID:   "unknown1",
			BlockType: 999,
			Children:  []string{"text1"},
		},
		"text1": {
			BlockID:   "text1",
			BlockType: btText,
			Text:      &richText{Elements: []element{{TextRun: &textRun{Content: "child content"}}}},
		},
	}

	var sb strings.Builder
	renderBlock(&sb, blockMap["unknown1"], blockMap, "", 0)
	got := sb.String()
	if !strings.Contains(got, "child content") {
		t.Errorf("unknown type should render children, got %q", got)
	}
}

// --- renderChildren with missing block test ---

func TestRenderChildrenMissingBlock(t *testing.T) {
	t.Parallel()

	blockMap := map[string]*blockData{}
	var sb strings.Builder
	renderChildren(&sb, []string{"nonexistent"}, blockMap, "", 0)
	if sb.String() != "" {
		t.Errorf("expected empty output for missing block, got %q", sb.String())
	}
}

// --- Block ID embedding test ---

func TestRenderBlockBidOnlyAtDepthZero(t *testing.T) {
	t.Parallel()

	block := &blockData{
		BlockID:   "myblock",
		BlockType: btText,
		Text:      &richText{Elements: []element{{TextRun: &textRun{Content: "text"}}}},
	}

	// Depth 0 - should have bid
	var sb0 strings.Builder
	renderBlock(&sb0, block, nil, "", 0)
	if !strings.Contains(sb0.String(), "<!-- bid:myblock -->") {
		t.Errorf("depth 0 should contain bid, got %q", sb0.String())
	}

	// Depth 1 - should NOT have bid
	var sb1 strings.Builder
	renderBlock(&sb1, block, nil, "", 1)
	if strings.Contains(sb1.String(), "<!-- bid:") {
		t.Errorf("depth 1 should not contain bid, got %q", sb1.String())
	}
}

// --- Code block language mapping test ---

func TestCodeLangMapping(t *testing.T) {
	t.Parallel()

	// Verify aliases point to correct feishu language codes
	cases := map[string]int{
		"sh":     7, // bash
		"bash":   7,
		"ts":     60, // typescript
		"js":     30, // javascript
		"py":     47, // python
		"golang": 22, // go
		"rb":     50, // ruby
		"rs":     51, // rust
		"kt":     32, // kotlin
		"yml":    66, // yaml
		"md":     38, // markdown
		"cs":     8,  // csharp
		"c++":    9,  // cpp
		"proto":  46, // protobuf
	}
	for alias, want := range cases {
		if got := codeLangIDs[alias]; got != want {
			t.Errorf("%s = %d, want %d", alias, got, want)
		}
	}

	// Verify primary names
	primaries := map[string]int{
		"go":         22,
		"python":     47,
		"javascript": 30,
		"typescript": 60,
		"java":       29,
		"c":          10,
		"cpp":        9,
		"csharp":     8,
		"rust":       51,
		"sql":        54,
		"html":       24,
		"css":        12,
		"json":       28,
		"xml":        65,
		"yaml":       66,
		"shell":      57,
		"swift":      58,
		"kotlin":     32,
		"ruby":       50,
		"php":        42,
	}
	for name, want := range primaries {
		if got := codeLangIDs[name]; got != want {
			t.Errorf("%s = %d, want %d", name, got, want)
		}
	}
}

// --- Roundtrip test: blocks -> markdown -> blocks ---

func TestBlocksToMarkdownRoundtrip(t *testing.T) {
	t.Parallel()

	raw := map[string]any{
		"items": []any{
			map[string]any{
				"block_id":   "page1",
				"block_type": btPage,
				"children":   []any{"h1", "t1", "b1"},
				"text":       map[string]any{"elements": []any{map[string]any{"text_run": map[string]any{"content": "Test Doc"}}}},
			},
			map[string]any{
				"block_id":   "h1",
				"block_type": btHeading4,
				"parent_id":  "page1",
				"heading4":   map[string]any{"elements": []any{map[string]any{"text_run": map[string]any{"content": "Section"}}}},
			},
			map[string]any{
				"block_id":   "t1",
				"block_type": btText,
				"parent_id":  "page1",
				"text":       map[string]any{"elements": []any{map[string]any{"text_run": map[string]any{"content": "Some text here"}}}},
			},
			map[string]any{
				"block_id":   "b1",
				"block_type": btBullet,
				"parent_id":  "page1",
				"bullet":     map[string]any{"elements": []any{map[string]any{"text_run": map[string]any{"content": "Bullet item"}}}},
			},
		},
	}

	md := blocksToMarkdown(raw, "")
	entries, err := markdownToBlocksWithIDs(md)
	if err != nil {
		t.Fatalf("roundtrip error: %v", err)
	}

	// Should have: title (heading), section heading, text, bullet
	// The title "# Test Doc" will be parsed as heading, section "#### Section" as heading, etc.
	if len(entries) < 3 {
		t.Fatalf("expected at least 3 entries, got %d", len(entries))
	}
}

// --- renderElements link decode error path ---

func TestRenderElementsLinkBadEncoding(t *testing.T) {
	t.Parallel()

	// Invalid percent-encoding should fall back to raw URL
	elements := []element{
		{TextRun: &textRun{Content: "link", Style: textRunStyle{Link: &link{URL: "https://example.com/%ZZ"}}}},
	}
	got := renderElements(elements)
	if got != "[link](https://example.com/%ZZ)" {
		t.Errorf("renderElements() = %q, want link with raw URL", got)
	}
}

// --- renderBlock nil content fields ---

func TestRenderBlockNilFields(t *testing.T) {
	t.Parallel()

	// Block types with nil content fields should produce only the bid marker
	tests := []struct {
		name  string
		block *blockData
	}{
		{"text nil", &blockData{BlockID: "b1", BlockType: btText}},
		{"heading nil", &blockData{BlockID: "b2", BlockType: btHeading1}},
		{"bullet nil", &blockData{BlockID: "b3", BlockType: btBullet}},
		{"ordered nil", &blockData{BlockID: "b4", BlockType: btOrdered}},
		{"todo nil", &blockData{BlockID: "b5", BlockType: btTodo}},
		{"quote nil", &blockData{BlockID: "b6", BlockType: btQuote}},
		{"code nil", &blockData{BlockID: "b7", BlockType: btCode}},
		{"image nil", &blockData{BlockID: "b8", BlockType: btImage}},
		{"file nil", &blockData{BlockID: "b9", BlockType: btFile}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sb strings.Builder
			renderBlock(&sb, tt.block, nil, "", 0)
			got := sb.String()
			// Should have bid marker but no content crash
			if !strings.Contains(got, "<!-- bid:") {
				t.Errorf("expected bid marker, got %q", got)
			}
		})
	}
}

// --- blocksToMarkdown no page block ---

func TestBlocksToMarkdownNoPageBlock(t *testing.T) {
	t.Parallel()

	raw := map[string]any{
		"items": []any{
			map[string]any{
				"block_id":   "text1",
				"block_type": btText,
				"text":       map[string]any{"elements": []any{map[string]any{"text_run": map[string]any{"content": "orphan"}}}},
			},
		},
	}

	got := blocksToMarkdown(raw, "")
	if got != "" {
		t.Errorf("expected empty output when no page block, got %q", got)
	}
}

// --- heading level full-replace path ---

func TestParseMarkdown_HeadingLevels_FullReplace(t *testing.T) {
	t.Parallel()

	md := "# H1\n\n## H2\n\n### H3\n\n#### H4\n"
	entries, err := parseMarkdownWithIDs(md)
	if err != nil {
		t.Fatalf("parseMarkdownWithIDs() error = %v", err)
	}
	want := []int{btHeading1, btHeading2, btHeading3, btHeading4}
	if len(entries) != len(want) {
		t.Fatalf("got %d entries, want %d", len(entries), len(want))
	}
	for i, e := range entries {
		if e.BlockType != want[i] {
			t.Errorf("entry[%d]: got block_type=%d, want %d", i, e.BlockType, want[i])
		}
	}
}

func TestParseMarkdown_HeadingLevels_WithBid(t *testing.T) {
	t.Parallel()

	md := "<!-- bid:b1 -->\n# H1\n\n<!-- bid:b2 -->\n## H2\n"
	entries, err := parseMarkdownWithIDs(md)
	if err != nil {
		t.Fatalf("parseMarkdownWithIDs() error = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if entries[0].BlockType != btHeading1 || entries[1].BlockType != btHeading2 {
		t.Errorf("with-bid path: got %d / %d, want btHeading1 / btHeading2", entries[0].BlockType, entries[1].BlockType)
	}
}

// --- link URL validation ---

func TestParseMarkdown_AnchorLink_Rejected(t *testing.T) {
	t.Parallel()

	md := "# T\n\n- [Section 1](#section-1)\n"
	_, err := parseMarkdownWithIDs(md)
	if err == nil {
		t.Fatal("expected error for anchor-only link, got nil")
	}
	if !strings.Contains(err.Error(), "line 3") {
		t.Errorf("error should report line number, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "anchor-only") {
		t.Errorf("error should mention anchor-only, got %q", err.Error())
	}
}

func TestParseMarkdown_RelativePathLink_Rejected(t *testing.T) {
	t.Parallel()

	md := "# T\n\nsee [other](../other.md)\n"
	_, err := parseMarkdownWithIDs(md)
	if err == nil {
		t.Fatal("expected error for relative-path link, got nil")
	}
	if !strings.Contains(err.Error(), "relative paths") {
		t.Errorf("error should mention relative paths, got %q", err.Error())
	}
}

func TestParseMarkdown_AbsoluteHTTPLink_OK(t *testing.T) {
	t.Parallel()

	md := "# T\n\nsee [google](https://google.com)\n"
	if _, err := parseMarkdownWithIDs(md); err != nil {
		t.Fatalf("absolute http link should be accepted, got %v", err)
	}
}

func TestParseMarkdown_MailtoLink_OK(t *testing.T) {
	t.Parallel()

	md := "# T\n\n[mail](mailto:a@b.com)\n"
	if _, err := parseMarkdownWithIDs(md); err != nil {
		t.Fatalf("mailto link should be accepted, got %v", err)
	}
}

// Suppress unused import warnings - these are used by existing tests
var (
	_ = context.Background
	_ = httptest.NewServer
	_ = time.Now
	_ = json.Marshal
	_ = fmt.Sprintf
)

func TestRenderElementsMentions(t *testing.T) {
	tests := []struct {
		name     string
		elements []element
		want     string
	}{
		{
			name: "document mention becomes a link",
			elements: []element{
				{TextRun: &textRun{Content: "见 "}},
				{MentionDoc: &mentionDoc{Token: "tok1", ObjType: 22, Title: "设计稿", URL: "https://xxx.feishu.cn/docx/tok1"}},
			},
			want: "见 [设计稿](https://xxx.feishu.cn/docx/tok1)",
		},
		{
			name:     "document mention falls back to the token",
			elements: []element{{MentionDoc: &mentionDoc{Token: "tok1", URL: "https://x/docx/tok1"}}},
			want:     "[tok1](https://x/docx/tok1)",
		},
		{
			name:     "document mention without a URL renders bare",
			elements: []element{{MentionDoc: &mentionDoc{Token: "tok1", Title: "设计稿"}}},
			want:     "设计稿",
		},
		{
			name:     "user mention renders as @id",
			elements: []element{{MentionUser: &mentionUser{UserID: "ou_abc"}}},
			want:     "@ou_abc",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := renderElements(tt.elements); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMentionsKeepExportAndDiffInSync(t *testing.T) {
	// A paragraph holding a mention must compare equal to its own markdown
	// export; otherwise `docs update` would rewrite the block and drop the chip.
	block := &blockData{
		BlockID:   "b1",
		BlockType: btText,
		Text: &richText{Elements: []element{
			{TextRun: &textRun{Content: "Owner: "}},
			{MentionUser: &mentionUser{UserID: "ou_abc"}},
		}},
	}
	comparable := blockComparableContent(block, map[string]*blockData{"b1": block})
	if comparable != "Owner: @ou_abc" {
		t.Fatalf("comparable = %q", comparable)
	}
}
