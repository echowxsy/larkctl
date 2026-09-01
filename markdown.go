package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// block types
const (
	btPage           = 1
	btText           = 2
	btHeading1       = 3
	btHeading2       = 4
	btHeading3       = 5
	btHeading4       = 6
	btHeading5       = 7
	btHeading6       = 8
	btHeading7       = 9
	btHeading8       = 10
	btHeading9       = 11
	btBullet         = 12
	btOrdered        = 13
	btCode           = 14
	btQuote          = 15 // legacy, use btQuoteContainer for new blocks
	btEquation       = 16
	btTodo           = 17
	btCallout        = 19
	btDiagram        = 21
	btDivider        = 22
	btFile           = 23
	btGrid           = 24
	btGridColumn     = 25
	btIframe         = 26
	btImage          = 27
	btTable          = 31
	btTableCell      = 32
	btView           = 33
	btQuoteContainer = 34
	btTask           = 35
	btBoard          = 43
	btLinkPreview    = 48
)

// code language IDs to names
var codeLangs = map[int]string{
	1:  "plaintext",
	2:  "abap",
	3:  "ada",
	4:  "apache",
	5:  "apex",
	6:  "assembly",
	7:  "bash",
	8:  "csharp",
	9:  "cpp",
	10: "c",
	11: "cobol",
	12: "css",
	13: "coffeescript",
	14: "d",
	15: "dart",
	16: "delphi",
	17: "django",
	18: "dockerfile",
	19: "erlang",
	20: "fortran",
	21: "foxpro",
	22: "go",
	23: "groovy",
	24: "html",
	25: "htmlbars",
	26: "http",
	27: "haskell",
	28: "json",
	29: "java",
	30: "javascript",
	31: "julia",
	32: "kotlin",
	33: "latex",
	34: "lisp",
	35: "lua",
	36: "matlab",
	37: "makefile",
	38: "markdown",
	39: "nginx",
	40: "objectivec",
	41: "openedgeabl",
	42: "php",
	43: "perl",
	44: "powershell",
	45: "prolog",
	46: "protobuf",
	47: "python",
	48: "r",
	49: "rpm",
	50: "ruby",
	51: "rust",
	52: "sas",
	53: "scss",
	54: "sql",
	55: "scala",
	56: "scheme",
	57: "shell",
	58: "swift",
	59: "thrift",
	60: "typescript",
	61: "vbscript",
	62: "verilog",
	63: "vhdl",
	64: "visualbasic",
	65: "xml",
	66: "yaml",
}

type blockData struct {
	BlockID        string            `json:"block_id"`
	BlockType      int               `json:"block_type"`
	ParentID       string            `json:"parent_id"`
	Children       []string          `json:"children,omitempty"`
	Text           *richText         `json:"text,omitempty"`
	Heading1       *richText         `json:"heading1,omitempty"`
	Heading2       *richText         `json:"heading2,omitempty"`
	Heading3       *richText         `json:"heading3,omitempty"`
	Heading4       *richText         `json:"heading4,omitempty"`
	Heading5       *richText         `json:"heading5,omitempty"`
	Heading6       *richText         `json:"heading6,omitempty"`
	Heading7       *richText         `json:"heading7,omitempty"`
	Heading8       *richText         `json:"heading8,omitempty"`
	Heading9       *richText         `json:"heading9,omitempty"`
	Bullet         *richText         `json:"bullet,omitempty"`
	Ordered        *richText         `json:"ordered,omitempty"`
	Quote          *richText         `json:"quote,omitempty"`
	Todo           *richText         `json:"todo,omitempty"`
	Code           *codeBlock        `json:"code,omitempty"`
	Image          *imageBlock       `json:"image,omitempty"`
	File           *fileBlock        `json:"file,omitempty"`
	Equation       *richText         `json:"equation,omitempty"`
	Table          *tableBlock       `json:"table,omitempty"`
	TableCell      *json.RawMessage  `json:"table_cell,omitempty"`
	QuoteContainer *json.RawMessage  `json:"quote_container,omitempty"`
	Callout        *calloutBlock     `json:"callout,omitempty"`
	Grid           *json.RawMessage  `json:"grid,omitempty"`
	GridColumn     *json.RawMessage  `json:"grid_column,omitempty"`
	Iframe         *json.RawMessage  `json:"iframe,omitempty"`
	Board          *json.RawMessage  `json:"board,omitempty"`
	LinkPreview    *linkPreviewBlock `json:"link_preview,omitempty"`
	View           *viewBlock        `json:"view,omitempty"`
}

type richText struct {
	Elements []element  `json:"elements"`
	Style    blockStyle `json:"style"`
}

type element struct {
	TextRun     *textRun     `json:"text_run,omitempty"`
	Equation    *equation    `json:"equation,omitempty"`
	MentionDoc  *mentionDoc  `json:"mention_doc,omitempty"`
	MentionUser *mentionUser `json:"mention_user,omitempty"`
}

// mentionDoc is the inline chip Feishu shows for a linked cloud document.
type mentionDoc struct {
	Token   string `json:"token"`
	ObjType int    `json:"obj_type"`
	Title   string `json:"title"`
	URL     string `json:"url"`
}

// mentionUser is the inline @-chip for a person. Feishu returns only the id,
// never a display name.
type mentionUser struct {
	UserID string `json:"user_id"`
}

type equation struct {
	Content string `json:"content"`
}

type textRun struct {
	Content string       `json:"content"`
	Style   textRunStyle `json:"text_element_style"`
}

type textRunStyle struct {
	Bold            bool  `json:"bold"`
	Italic          bool  `json:"italic"`
	Strikethrough   bool  `json:"strikethrough"`
	Underline       bool  `json:"underline"`
	InlineCode      bool  `json:"inline_code"`
	BackgroundColor int   `json:"background_color,omitempty"`
	Link            *link `json:"link,omitempty"`
}

type link struct {
	URL string `json:"url"`
}

type blockStyle struct {
	Align    int    `json:"align"`
	Done     bool   `json:"done"`
	Sequence string `json:"sequence"`
}

type codeBlock struct {
	Elements []element `json:"elements"`
	Style    codeStyle `json:"style"`
}

type codeStyle struct {
	Language int `json:"language"`
}

type imageBlock struct {
	Token  string `json:"token"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type fileBlock struct {
	Token string `json:"token"`
	Name  string `json:"name"`
}

type tableBlock struct {
	Cells    []string      `json:"cells"`
	Property tableProperty `json:"property"`
}

type tableProperty struct {
	ColumnSize int `json:"column_size"`
	RowCount   int `json:"row_count"`
}

type calloutBlock struct {
	BackgroundColor int    `json:"background_color"`
	BorderColor     int    `json:"border_color"`
	EmojiID         string `json:"emoji_id,omitempty"`
}

type linkPreviewBlock struct {
	URL string `json:"url"`
}

type viewBlock struct {
	ViewType int `json:"view_type"`
}

func blocksToMarkdown(raw any, imageDir string) string {
	b, _ := json.Marshal(raw)
	var resp struct {
		Items []blockData `json:"items"`
	}
	if json.Unmarshal(b, &resp) != nil {
		return ""
	}

	blockMap := map[string]*blockData{}
	for i := range resp.Items {
		blockMap[resp.Items[i].BlockID] = &resp.Items[i]
	}

	var sb strings.Builder
	var pageBlock *blockData
	for i := range resp.Items {
		if resp.Items[i].BlockType == btPage {
			pageBlock = &resp.Items[i]
			break
		}
	}
	if pageBlock == nil {
		return ""
	}

	// Render title
	if pageBlock.Text != nil || pageBlock.Heading1 != nil {
		rt := pageBlock.Text
		if rt == nil {
			rt = pageBlock.Heading1
		}
		title := renderElements(rt.Elements)
		if title != "" {
			sb.WriteString("# " + title + "\n\n")
		}
	} else {
		// Page block stores title in "page" field which maps to elements
		// Try raw extraction
		var rawBlock map[string]any
		rawBytes, _ := json.Marshal(pageBlock)
		_ = json.Unmarshal(rawBytes, &rawBlock)
		if page, ok := rawBlock["page"].(map[string]any); ok {
			if elems, ok := page["elements"].([]any); ok {
				title := renderRawElements(elems)
				if title != "" {
					sb.WriteString("# " + title + "\n\n")
				}
			}
		}
	}

	renderChildren(&sb, pageBlock.Children, blockMap, imageDir, 0)
	return sb.String()
}

func renderChildren(sb *strings.Builder, children []string, blockMap map[string]*blockData, imageDir string, depth int) {
	for _, childID := range children {
		block, ok := blockMap[childID]
		if !ok {
			continue
		}
		renderBlock(sb, block, blockMap, imageDir, depth)
	}
}

func renderBlock(sb *strings.Builder, block *blockData, blockMap map[string]*blockData, imageDir string, depth int) {
	// Embed block ID for diff-based update (only for top-level renderable blocks)
	if depth == 0 && block.BlockID != "" {
		sb.WriteString("<!-- bid:" + block.BlockID + " -->\n")
	}

	switch block.BlockType {
	case btText:
		if block.Text != nil {
			text := renderElements(block.Text.Elements)
			sb.WriteString(text + "\n\n")
		}

	case btHeading1, btHeading2, btHeading3, btHeading4, btHeading5, btHeading6, btHeading7, btHeading8, btHeading9:
		level := block.BlockType - btHeading1 + 1
		rt := getHeadingRT(block)
		if rt != nil {
			text := renderElements(rt.Elements)
			sb.WriteString(strings.Repeat("#", level) + " " + text + "\n\n")
		}

	case btBullet:
		if block.Bullet != nil {
			indent := strings.Repeat("  ", depth)
			text := renderElements(block.Bullet.Elements)
			sb.WriteString(indent + "- " + text + "\n")
			if len(block.Children) > 0 {
				renderChildren(sb, block.Children, blockMap, imageDir, depth+1)
			}
		}

	case btOrdered:
		if block.Ordered != nil {
			indent := strings.Repeat("  ", depth)
			seq := block.Ordered.Style.Sequence
			if seq == "" || seq == "auto" || seq == "undefined" {
				seq = "1"
			}
			text := renderElements(block.Ordered.Elements)
			sb.WriteString(indent + seq + ". " + text + "\n")
			if len(block.Children) > 0 {
				renderChildren(sb, block.Children, blockMap, imageDir, depth+1)
			}
		}

	case btTodo:
		if block.Todo != nil {
			check := "[ ]"
			if block.Todo.Style.Done {
				check = "[x]"
			}
			text := renderElements(block.Todo.Elements)
			sb.WriteString("- " + check + " " + text + "\n")
		}

	case btQuote:
		if block.Quote != nil {
			text := renderElements(block.Quote.Elements)
			sb.WriteString("> " + text + "\n\n")
		}

	case btQuoteContainer:
		// Quote container holds child text blocks
		for _, childID := range block.Children {
			child := blockMap[childID]
			if child == nil {
				continue
			}
			rt := getContentRT(child)
			if rt != nil {
				text := renderElements(rt.Elements)
				sb.WriteString("> " + text + "\n")
			}
		}
		sb.WriteString("\n")

	case btCode:
		if block.Code != nil {
			lang := codeLangs[block.Code.Style.Language]
			var code strings.Builder
			for _, el := range block.Code.Elements {
				if el.TextRun != nil {
					code.WriteString(el.TextRun.Content)
				}
			}
			sb.WriteString("```" + lang + "\n" + code.String() + "\n```\n\n")
		}

	case btDivider:
		sb.WriteString("---\n\n")

	case btImage:
		if block.Image != nil {
			if imageDir != "" {
				sb.WriteString(fmt.Sprintf("![image](%s/%s)\n\n", imageDir, block.Image.Token))
			} else {
				sb.WriteString(fmt.Sprintf("![image](feishu-media://%s)\n\n", block.Image.Token))
			}
		}

	case btFile:
		if block.File != nil {
			sb.WriteString(fmt.Sprintf("[%s](feishu-file://%s)\n\n", block.File.Name, block.File.Token))
		}

	case btTable:
		renderTable(sb, block, blockMap)

	case btEquation:
		if block.Equation != nil {
			var formula strings.Builder
			for _, el := range block.Equation.Elements {
				if el.TextRun != nil {
					formula.WriteString(el.TextRun.Content)
				}
				if el.Equation != nil {
					formula.WriteString(el.Equation.Content)
				}
			}
			sb.WriteString("$$\n" + formula.String() + "\n$$\n\n")
		}

	case btCallout:
		// Callout container - render as GitHub-style admonition
		calloutType := "NOTE"
		if block.Callout != nil {
			switch block.Callout.BackgroundColor {
			case 2:
				calloutType = "WARNING"
			case 3:
				calloutType = "CAUTION"
			case 4:
				calloutType = "TIP"
			case 5:
				calloutType = "TIP"
			case 7:
				calloutType = "IMPORTANT"
			}
		}
		sb.WriteString("> [!" + calloutType + "]\n")
		for _, childID := range block.Children {
			child := blockMap[childID]
			if child == nil {
				continue
			}
			rt := getContentRT(child)
			if rt != nil {
				text := renderElements(rt.Elements)
				sb.WriteString("> " + text + "\n")
			}
		}
		sb.WriteString("\n")

	case btGrid:
		// Grid layout - render columns sequentially
		for _, childID := range block.Children {
			child := blockMap[childID]
			if child != nil && child.BlockType == btGridColumn {
				renderChildren(sb, child.Children, blockMap, imageDir, depth)
			}
		}

	case btGridColumn:
		// Grid column - render children directly
		renderChildren(sb, block.Children, blockMap, imageDir, depth)

	case btIframe:
		// Embedded iframe - render as link if URL available
		if len(block.Children) > 0 {
			renderChildren(sb, block.Children, blockMap, imageDir, depth)
		}

	case btLinkPreview:
		if block.LinkPreview != nil && block.LinkPreview.URL != "" {
			u := block.LinkPreview.URL
			decoded, err := url.QueryUnescape(u)
			if err == nil {
				u = decoded
			}
			sb.WriteString(u + "\n\n")
		}

	case btDiagram:
		// Diagram block - cannot extract content via API, note it
		sb.WriteString("[Diagram]\n\n")

	case btTask:
		// Task block - read-only, note it
		sb.WriteString("[Task]\n\n")

	case btBoard:
		// Board/whiteboard - read-only, note it
		sb.WriteString("[Board]\n\n")

	case btView:
		if len(block.Children) > 0 {
			renderChildren(sb, block.Children, blockMap, imageDir, depth)
		}

	default:
		// For unknown types with children, try rendering children
		if len(block.Children) > 0 {
			renderChildren(sb, block.Children, blockMap, imageDir, depth)
		}
	}
}

func renderTable(sb *strings.Builder, block *blockData, blockMap map[string]*blockData) {
	if block.Table == nil {
		return
	}
	cols := block.Table.Property.ColumnSize
	if cols <= 0 {
		return
	}
	cells := block.Table.Cells
	rows := len(cells) / cols

	for r := 0; r < rows; r++ {
		sb.WriteString("|")
		for c := 0; c < cols; c++ {
			idx := r*cols + c
			cellText := ""
			if idx < len(cells) {
				cellBlock, ok := blockMap[cells[idx]]
				if ok {
					cellText = renderCellContent(cellBlock, blockMap)
				}
			}
			sb.WriteString(" " + strings.ReplaceAll(cellText, "\n", " ") + " |")
		}
		sb.WriteString("\n")
		if r == 0 {
			sb.WriteString("|")
			for c := 0; c < cols; c++ {
				sb.WriteString(" --- |")
			}
			sb.WriteString("\n")
		}
	}
	sb.WriteString("\n")
}

func renderCellContent(cell *blockData, blockMap map[string]*blockData) string {
	var parts []string
	for _, childID := range cell.Children {
		child, ok := blockMap[childID]
		if !ok {
			continue
		}
		rt := getContentRT(child)
		if rt != nil {
			parts = append(parts, renderElements(rt.Elements))
		}
	}
	return strings.Join(parts, " ")
}

func getHeadingRT(b *blockData) *richText {
	switch b.BlockType {
	case btHeading1:
		return b.Heading1
	case btHeading2:
		return b.Heading2
	case btHeading3:
		return b.Heading3
	case btHeading4:
		return b.Heading4
	case btHeading5:
		return b.Heading5
	case btHeading6:
		return b.Heading6
	case btHeading7:
		return b.Heading7
	case btHeading8:
		return b.Heading8
	case btHeading9:
		return b.Heading9
	}
	return nil
}

func getContentRT(b *blockData) *richText {
	switch b.BlockType {
	case btText:
		return b.Text
	case btBullet:
		return b.Bullet
	case btOrdered:
		return b.Ordered
	case btQuote:
		return b.Quote
	case btTodo:
		return b.Todo
	default:
		return getHeadingRT(b)
	}
}

// renderMentionDoc renders a document mention as a markdown link.
func renderMentionDoc(m *mentionDoc) string {
	title := m.Title
	if title == "" {
		title = m.Token
	}
	target := m.URL
	if decoded, err := url.QueryUnescape(target); err == nil {
		target = decoded
	}
	if target == "" {
		return title
	}
	return "[" + title + "](" + target + ")"
}

func renderElements(elements []element) string {
	var sb strings.Builder
	for _, el := range elements {
		// Inline equation
		if el.Equation != nil {
			sb.WriteString("$" + el.Equation.Content + "$")
			continue
		}
		// Mentions have no markdown equivalent. Rendering them as a link (docs)
		// or an @id (people) keeps them visible on export and keeps the export
		// and the diff comparable in sync, so an untouched paragraph is not
		// rewritten — a rewrite would drop the mention.
		if el.MentionDoc != nil {
			sb.WriteString(renderMentionDoc(el.MentionDoc))
			continue
		}
		if el.MentionUser != nil {
			sb.WriteString("@" + el.MentionUser.UserID)
			continue
		}
		if el.TextRun == nil {
			continue
		}
		content := el.TextRun.Content
		style := el.TextRun.Style

		if style.InlineCode {
			sb.WriteString("`" + content + "`")
			continue
		}

		if style.Link != nil && style.Link.URL != "" {
			decoded, err := url.QueryUnescape(style.Link.URL)
			if err != nil {
				decoded = style.Link.URL
			}
			content = "[" + content + "](" + decoded + ")"
		}
		if style.Bold {
			content = "**" + content + "**"
		}
		if style.Italic {
			content = "*" + content + "*"
		}
		if style.Strikethrough {
			content = "~~" + content + "~~"
		}
		if style.BackgroundColor > 0 {
			content = "==" + content + "=="
		}
		sb.WriteString(content)
	}
	return sb.String()
}

// --- Markdown → Blocks (import) ---

// reverse map: language name → code
var codeLangIDs map[string]int

func init() {
	codeLangIDs = make(map[string]int, len(codeLangs))
	for id, name := range codeLangs {
		if name != "" {
			codeLangIDs[name] = id
		}
	}
	// aliases
	codeLangIDs["sh"] = 7      // bash
	codeLangIDs["c++"] = 9     // cpp
	codeLangIDs["cs"] = 8      // csharp
	codeLangIDs["ts"] = 60     // typescript
	codeLangIDs["js"] = 30     // javascript
	codeLangIDs["py"] = 47     // python
	codeLangIDs["rb"] = 50     // ruby
	codeLangIDs["rs"] = 51     // rust
	codeLangIDs["kt"] = 32     // kotlin
	codeLangIDs["tex"] = 33    // latex
	codeLangIDs["make"] = 37   // makefile
	codeLangIDs["md"] = 38     // markdown
	codeLangIDs["objc"] = 40   // objectivec
	codeLangIDs["ps1"] = 44    // powershell
	codeLangIDs["proto"] = 46  // protobuf
	codeLangIDs["vb"] = 64     // visualbasic
	codeLangIDs["yml"] = 66    // yaml
	codeLangIDs["golang"] = 22 // go
}

// validateLinkURL rejects link targets that Feishu's docs API doesn't accept.
// Feishu only accepts absolute URLs with http/https/mailto scheme; relative
// paths and bare anchors trigger server-side schema mismatch (code 1770006).
func validateLinkURL(s string) error {
	if strings.HasPrefix(s, "#") {
		return errors.New("anchor-only links are not supported by Feishu (rewrite as plain text)")
	}
	u, err := url.Parse(s)
	if err != nil {
		return fmt.Errorf("not a valid URL: %w", err)
	}
	switch u.Scheme {
	case "http", "https", "mailto":
		return nil
	case "":
		return errors.New("relative paths are not supported by Feishu (use absolute http/https/mailto)")
	default:
		return fmt.Errorf("unsupported URL scheme %q (Feishu only accepts http/https/mailto)", u.Scheme)
	}
}

// parseInlineMarkdown parses bold, italic, strikethrough, highlight, inline code, and links into Feishu text elements.
func parseInlineMarkdown(text string, lineNo int) ([]any, error) {
	var elements []any
	i := 0

	addTextRun := func(content string, style map[string]any) {
		if style == nil {
			style = map[string]any{}
		}
		elements = append(elements, map[string]any{
			"text_run": map[string]any{
				"content":            content,
				"text_element_style": style,
			},
		})
	}

	for i < len(text) {
		// Bold+Italic: ***text***
		if i+3 <= len(text) && text[i:i+3] == "***" {
			end := strings.Index(text[i+3:], "***")
			if end >= 0 {
				addTextRun(text[i+3:i+3+end], map[string]any{"bold": true, "italic": true})
				i = i + 3 + end + 3
				continue
			}
		}

		// Strikethrough: ~~text~~
		if i+2 <= len(text) && text[i:i+2] == "~~" {
			end := strings.Index(text[i+2:], "~~")
			if end >= 0 {
				addTextRun(text[i+2:i+2+end], map[string]any{"strikethrough": true})
				i = i + 2 + end + 2
				continue
			}
		}

		// Highlight: ==text==
		if i+2 <= len(text) && text[i:i+2] == "==" {
			end := strings.Index(text[i+2:], "==")
			if end >= 0 {
				addTextRun(text[i+2:i+2+end], map[string]any{"background_color": 3})
				i = i + 2 + end + 2
				continue
			}
		}

		// Bold: **text**
		if i+2 <= len(text) && text[i:i+2] == "**" {
			end := strings.Index(text[i+2:], "**")
			if end >= 0 {
				addTextRun(text[i+2:i+2+end], map[string]any{"bold": true})
				i = i + 2 + end + 2
				continue
			}
		}

		// Italic: *text* (single asterisk, not **)
		if text[i] == '*' && (i+1 >= len(text) || text[i+1] != '*') {
			end := strings.Index(text[i+1:], "*")
			if end >= 0 {
				addTextRun(text[i+1:i+1+end], map[string]any{"italic": true})
				i = i + 1 + end + 1
				continue
			}
		}

		// Inline code: `text`
		if text[i] == '`' {
			end := strings.Index(text[i+1:], "`")
			if end >= 0 {
				addTextRun(text[i+1:i+1+end], map[string]any{"inline_code": true})
				i = i + 1 + end + 1
				continue
			}
		}

		// Link: [text](url)
		if text[i] == '[' {
			closeBracket := strings.Index(text[i:], "](")
			if closeBracket >= 0 {
				closePos := closeBracket + i
				parenEnd := strings.Index(text[closePos+2:], ")")
				if parenEnd >= 0 {
					linkText := text[i+1 : closePos]
					linkURL := text[closePos+2 : closePos+2+parenEnd]
					if err := validateLinkURL(linkURL); err != nil {
						return nil, fmt.Errorf("line %d: invalid link URL %q: %w", lineNo, linkURL, err)
					}
					// Feishu requires percent-encoded URLs
					encodedURL := url.QueryEscape(linkURL)
					addTextRun(linkText, map[string]any{"link": map[string]any{"url": encodedURL}})
					i = closePos + 2 + parenEnd + 1
					continue
				}
			}
		}

		// Plain text: consume until next special char
		end := len(text)
		for _, marker := range []string{"***", "**", "~~", "==", "*", "`", "["} {
			pos := strings.Index(text[i+1:], marker)
			if pos >= 0 && i+1+pos < end {
				end = i + 1 + pos
			}
		}
		addTextRun(text[i:end], nil)
		i = end
	}

	if len(elements) == 0 {
		return []any{map[string]any{"text_run": map[string]any{"content": text}}}, nil
	}
	return elements, nil
}

// mdBlockEntry represents a markdown section mapped to a Feishu block.
type mdBlockEntry struct {
	BlockID    string         // existing block ID (empty = new block)
	BlockType  int            // Feishu block type
	Body       map[string]any // block body for create or update (nil = preserve-only)
	Comparable string         // normalized content used for change detection
	SourceLine int            // starting line number in markdown (1-based)
	Children   []mdBlockEntry // nested child blocks (e.g. nested list items)
}

// parseMarkdownWithIDs parses markdown that may contain <!-- bid:xxx --> markers,
// returning structured entries for diff-based update.
func parseMarkdownWithIDs(md string) ([]mdBlockEntry, error) {
	return markdownToBlocksWithIDs(md)
}

// markdownToBlocksWithIDs converts markdown to Feishu DocX blocks, preserving <!-- bid:xxx --> markers.
func markdownToBlocksWithIDs(md string) ([]mdBlockEntry, error) {
	var entries []mdBlockEntry
	lines := strings.Split(md, "\n")
	currentBID := ""

	for i := 0; i < len(lines); {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		lineNo := i + 1

		// Block ID marker
		if strings.HasPrefix(line, "<!-- bid:") && strings.HasSuffix(line, " -->") {
			currentBID = strings.TrimSuffix(strings.TrimPrefix(line, "<!-- bid:"), " -->")
			i++
			continue
		}

		// Keep block markers attached to the next non-empty content line.
		if trimmed == "" {
			i++
			continue
		}

		bid := currentBID
		currentBID = "" // consume

		// Block equation: $$...$$ — Feishu API rejects equation blocks (type 16) on batch_create,
		// so we render as a code block with LaTeX language as a workaround.
		if trimmed == "$$" {
			var formulaLines []string
			i++
			for i < len(lines) && strings.TrimSpace(lines[i]) != "$$" {
				formulaLines = append(formulaLines, lines[i])
				i++
			}
			if i < len(lines) {
				i++
			}
			formula := strings.Join(formulaLines, "\n")
			langCode := codeLangIDs["latex"]
			entries = append(entries, mdBlockEntry{
				BlockID:    bid,
				BlockType:  btCode,
				Comparable: fmt.Sprintf("%d\n%s", langCode, formula),
				SourceLine: lineNo,
				Body: map[string]any{
					"block_type": btCode,
					"code": map[string]any{
						"elements": []any{map[string]any{"text_run": map[string]any{"content": formula}}},
						"style":    map[string]any{"language": langCode},
					},
				},
			})
			continue
		}

		// Code block
		if strings.HasPrefix(line, "```") {
			langName := strings.TrimSpace(strings.ToLower(line[3:]))
			langCode := 1
			if id, ok := codeLangIDs[langName]; ok {
				langCode = id
			}
			var codeLines []string
			i++
			for i < len(lines) && !strings.HasPrefix(lines[i], "```") {
				codeLines = append(codeLines, lines[i])
				i++
			}
			if i < len(lines) {
				i++
			}
			code := strings.Join(codeLines, "\n")
			if code != "" {
				entries = append(entries, mdBlockEntry{
					BlockID:    bid,
					BlockType:  btCode,
					Comparable: fmt.Sprintf("%d\n%s", langCode, code),
					SourceLine: lineNo,
					Body: map[string]any{
						"block_type": btCode,
						"code": map[string]any{
							"elements": []any{map[string]any{"text_run": map[string]any{"content": code}}},
							"style":    map[string]any{"language": langCode},
						},
					},
				})
			}
			continue
		}

		// Tables are preserved if unchanged, but not diff-editable.
		if strings.HasPrefix(trimmed, "|") {
			start := i
			var tableLines []string
			for i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), "|") {
				tableLines = append(tableLines, strings.TrimSpace(lines[i]))
				i++
			}
			if bid == "" {
				return nil, fmt.Errorf("line %d: creating or moving tables is not supported by docs update", lineNo)
			}
			entries = append(entries, mdBlockEntry{
				BlockID:    bid,
				BlockType:  btTable,
				Comparable: strings.Join(tableLines, "\n"),
				SourceLine: start + 1,
			})
			continue
		}

		if token, ok := parseMarkdownImageToken(trimmed); ok {
			if bid == "" {
				return nil, fmt.Errorf("line %d: creating or moving images is not supported by docs update", lineNo)
			}
			entries = append(entries, mdBlockEntry{
				BlockID:    bid,
				BlockType:  btImage,
				Comparable: "image:" + token,
				SourceLine: lineNo,
			})
			i++
			continue
		}

		if name, token, ok := parseMarkdownFileLink(trimmed); ok {
			if bid == "" {
				return nil, fmt.Errorf("line %d: creating or moving file links is not supported by docs update", lineNo)
			}
			entries = append(entries, mdBlockEntry{
				BlockID:    bid,
				BlockType:  btFile,
				Comparable: "file:" + name + "\t" + token,
				SourceLine: lineNo,
			})
			i++
			continue
		}

		if trimmed == "---" {
			entries = append(entries, mdBlockEntry{
				BlockID:    bid,
				BlockType:  btDivider,
				Comparable: "divider",
				SourceLine: lineNo,
				Body: map[string]any{
					"block_type": btDivider,
					"divider":    map[string]any{},
				},
			})
			i++
			continue
		}

		// Headings
		if strings.HasPrefix(line, "#") {
			level := 0
			for _, c := range line {
				if c == '#' {
					level++
				} else {
					break
				}
			}
			if level > 0 && level <= 9 {
				text := strings.TrimSpace(line[level:])
				if text != "" {
					blockType := headingBlockType(level)
					field := headingFieldName(blockType)
					elements, err := parseInlineMarkdown(text, lineNo)
					if err != nil {
						return nil, err
					}
					entries = append(entries, mdBlockEntry{
						BlockID:    bid,
						BlockType:  blockType,
						Comparable: text,
						SourceLine: lineNo,
						Body: map[string]any{
							"block_type": blockType,
							field:        map[string]any{"elements": elements},
						},
					})
				}
				i++
				continue
			}
		}

		// List items (bullet, ordered, todo), with nesting support
		if isListLine(line) {
			listEntries, consumed, err := parseListBlock(lines, i, bid)
			if err != nil {
				return nil, err
			}
			entries = append(entries, listEntries...)
			i += consumed
			continue
		}

		if strings.HasPrefix(trimmed, "> ") || trimmed == ">" {
			// Collect consecutive quote lines
			var quoteChildren []mdBlockEntry
			var quoteTexts []string
			isCallout := false
			calloutColor := 6 // default: blue/info
			firstLine := true
			for i < len(lines) {
				lt := strings.TrimSpace(lines[i])
				if strings.HasPrefix(lt, "> ") {
					text := strings.TrimSpace(lt[2:])
					// Skip nested quote markers (flatten >> to >)
					for strings.HasPrefix(text, "> ") {
						text = strings.TrimSpace(text[2:])
					}
					// Detect callout syntax: > [!TYPE]
					if firstLine && strings.HasPrefix(text, "[!") && strings.Contains(text, "]") {
						closeIdx := strings.Index(text, "]")
						calloutType := strings.ToUpper(text[2:closeIdx])
						isCallout = true
						switch calloutType {
						case "WARNING":
							calloutColor = 2
						case "CAUTION":
							calloutColor = 3
						case "TIP", "SUCCESS":
							calloutColor = 4
						case "IMPORTANT":
							calloutColor = 7
						default: // NOTE, INFO
							calloutColor = 6
						}
						// Text after ] is part of the callout content
						afterBracket := strings.TrimSpace(text[closeIdx+1:])
						if afterBracket != "" {
							quoteTexts = append(quoteTexts, afterBracket)
							elements, err := parseInlineMarkdown(afterBracket, i+1)
							if err != nil {
								return nil, err
							}
							quoteChildren = append(quoteChildren, mdBlockEntry{
								BlockType:  btText,
								Comparable: afterBracket,
								SourceLine: i + 1,
								Body: map[string]any{
									"block_type": btText,
									"text":       map[string]any{"elements": elements},
								},
							})
						}
						i++
						firstLine = false
						continue
					}
					firstLine = false
					quoteTexts = append(quoteTexts, text)
					elements, err := parseInlineMarkdown(text, i+1)
					if err != nil {
						return nil, err
					}
					quoteChildren = append(quoteChildren, mdBlockEntry{
						BlockType:  btText,
						Comparable: text,
						SourceLine: i + 1,
						Body: map[string]any{
							"block_type": btText,
							"text":       map[string]any{"elements": elements},
						},
					})
					i++
				} else if lt == ">" {
					firstLine = false
					quoteTexts = append(quoteTexts, "")
					elements, err := parseInlineMarkdown("", i+1)
					if err != nil {
						return nil, err
					}
					quoteChildren = append(quoteChildren, mdBlockEntry{
						BlockType:  btText,
						Comparable: "",
						SourceLine: i + 1,
						Body: map[string]any{
							"block_type": btText,
							"text":       map[string]any{"elements": elements},
						},
					})
					i++
				} else {
					break
				}
			}

			if isCallout {
				entries = append(entries, mdBlockEntry{
					BlockID:    bid,
					BlockType:  btCallout,
					Comparable: "callout:" + strings.Join(quoteTexts, "\n"),
					SourceLine: lineNo,
					Children:   quoteChildren,
					Body: map[string]any{
						"block_type": btCallout,
						"callout": map[string]any{
							"background_color": calloutColor,
							"border_color":     calloutColor,
						},
					},
				})
			} else {
				entries = append(entries, mdBlockEntry{
					BlockID:    bid,
					BlockType:  btQuoteContainer,
					Comparable: strings.Join(quoteTexts, "\n"),
					SourceLine: lineNo,
					Children:   quoteChildren,
					Body: map[string]any{
						"block_type":      btQuoteContainer,
						"quote_container": map[string]any{},
					},
				})
			}
			continue
		}

		// Regular text
		elements, err := parseInlineMarkdown(line, lineNo)
		if err != nil {
			return nil, err
		}
		entries = append(entries, mdBlockEntry{
			BlockID:    bid,
			BlockType:  btText,
			Comparable: line,
			SourceLine: lineNo,
			Body: map[string]any{
				"block_type": btText,
				"text":       map[string]any{"elements": elements},
			},
		})
		i++
	}

	return entries, nil
}

func headingBlockType(level int) int {
	switch level {
	case 1:
		return btHeading1
	case 2:
		return btHeading2
	case 3:
		return btHeading3
	case 4:
		return btHeading4
	case 5:
		return btHeading5
	case 6:
		return btHeading6
	case 7:
		return btHeading7
	case 8:
		return btHeading8
	case 9:
		return btHeading9
	default:
		return btHeading4
	}
}

func headingFieldName(blockType int) string {
	switch blockType {
	case btHeading1:
		return "heading1"
	case btHeading2:
		return "heading2"
	case btHeading3:
		return "heading3"
	case btHeading4:
		return "heading4"
	case btHeading5:
		return "heading5"
	case btHeading6:
		return "heading6"
	case btHeading7:
		return "heading7"
	case btHeading8:
		return "heading8"
	case btHeading9:
		return "heading9"
	default:
		return "heading4"
	}
}

func isHeadingBlockType(blockType int) bool {
	return blockType >= btHeading1 && blockType <= btHeading9
}

// isListLine returns true if the line is a bullet or ordered list item (at any indentation).
func isListLine(line string) bool {
	trimmed := strings.TrimLeft(line, " ")
	if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "- [") {
		return true
	}
	_, _, ok := parseMarkdownOrderedItem(trimmed)
	return ok
}

// listIndent returns the number of leading spaces.
func listIndent(line string) int {
	return len(line) - len(strings.TrimLeft(line, " "))
}

// parseListBlock parses a consecutive block of list items (bullet/ordered) starting at index start,
// building a tree structure for nested items. Returns the parsed entries and how many lines were consumed.
func parseListBlock(lines []string, start int, firstBID string) ([]mdBlockEntry, int, error) {
	type stackItem struct {
		indent int
		entry  *mdBlockEntry
	}

	var topEntries []mdBlockEntry
	var stack []stackItem
	consumed := 0

	for i := start; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			// blank line ends the list block (check if next line is still a list)
			if i+1 < len(lines) && isListLine(lines[i+1]) {
				consumed++
				continue
			}
			break
		}
		if !isListLine(line) {
			break
		}
		consumed++

		indent := listIndent(line)
		content := strings.TrimLeft(line, " ")
		lineNo := i + 1

		var entry mdBlockEntry

		// Determine list item type
		if strings.HasPrefix(content, "- [ ] ") || strings.HasPrefix(strings.ToLower(content), "- [x] ") {
			done := strings.ToLower(content[3:4]) == "x"
			text := strings.TrimSpace(content[6:])
			elements, err := parseInlineMarkdown(text, lineNo)
			if err != nil {
				return nil, 0, err
			}
			entry = mdBlockEntry{
				BlockType:  btTodo,
				Comparable: content[2:],
				SourceLine: lineNo,
				Body: map[string]any{
					"block_type": btTodo,
					"todo": map[string]any{
						"elements": elements,
						"style":    map[string]any{"done": done},
					},
				},
			}
		} else if strings.HasPrefix(content, "- ") {
			text := strings.TrimSpace(content[2:])
			elements, err := parseInlineMarkdown(text, lineNo)
			if err != nil {
				return nil, 0, err
			}
			entry = mdBlockEntry{
				BlockType:  btBullet,
				Comparable: text,
				SourceLine: lineNo,
				Body: map[string]any{
					"block_type": btBullet,
					"bullet":     map[string]any{"elements": elements},
				},
			}
		} else if seq, text, ok := parseMarkdownOrderedItem(content); ok {
			elements, err := parseInlineMarkdown(text, lineNo)
			if err != nil {
				return nil, 0, err
			}
			entry = mdBlockEntry{
				BlockType:  btOrdered,
				Comparable: seq + ". " + text,
				SourceLine: lineNo,
				Body: map[string]any{
					"block_type": btOrdered,
					"ordered": map[string]any{
						"elements": elements,
						"style":    map[string]any{"sequence": seq},
					},
				},
			}
		} else {
			break
		}

		// Apply bid only to first item
		if consumed == 1 && firstBID != "" {
			entry.BlockID = firstBID
		}

		// Pop stack items with indent >= current (find parent)
		for len(stack) > 0 && stack[len(stack)-1].indent >= indent {
			stack = stack[:len(stack)-1]
		}

		if len(stack) == 0 {
			// Top-level item
			topEntries = append(topEntries, entry)
			stack = append(stack, stackItem{indent: indent, entry: &topEntries[len(topEntries)-1]})
		} else {
			// Child of the last stack item
			parent := stack[len(stack)-1].entry
			parent.Children = append(parent.Children, entry)
			stack = append(stack, stackItem{indent: indent, entry: &parent.Children[len(parent.Children)-1]})
		}
	}

	return topEntries, consumed, nil
}

func parseMarkdownOrderedItem(line string) (string, string, bool) {
	dot := strings.Index(line, ". ")
	if dot <= 0 {
		return "", "", false
	}
	seq := line[:dot]
	for _, c := range seq {
		if c < '0' || c > '9' {
			return "", "", false
		}
	}
	text := strings.TrimSpace(line[dot+2:])
	if text == "" {
		return "", "", false
	}
	return seq, text, true
}

func parseMarkdownImageToken(line string) (string, bool) {
	if !strings.HasPrefix(line, "![") {
		return "", false
	}
	sep := strings.Index(line, "](")
	if sep < 0 || !strings.HasSuffix(line, ")") {
		return "", false
	}
	ref := line[sep+2 : len(line)-1]
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", false
	}
	if strings.HasPrefix(ref, "feishu-media://") {
		return strings.TrimPrefix(ref, "feishu-media://"), true
	}
	return extractToken(ref), true
}

func parseMarkdownFileLink(line string) (string, string, bool) {
	if !strings.HasPrefix(line, "[") {
		return "", "", false
	}
	sep := strings.Index(line, "](")
	if sep <= 1 || !strings.HasSuffix(line, ")") {
		return "", "", false
	}
	name := line[1:sep]
	ref := line[sep+2 : len(line)-1]
	if !strings.HasPrefix(ref, "feishu-file://") {
		return "", "", false
	}
	token := strings.TrimPrefix(ref, "feishu-file://")
	if token == "" {
		return "", "", false
	}
	return name, token, true
}

func blockComparableContent(block *blockData, blockMap map[string]*blockData) string {
	switch block.BlockType {
	case btText:
		if block.Text != nil {
			return renderElements(block.Text.Elements)
		}
	case btBullet:
		if block.Bullet != nil {
			return renderElements(block.Bullet.Elements)
		}
	case btOrdered:
		if block.Ordered != nil {
			seq := block.Ordered.Style.Sequence
			if seq == "" || seq == "auto" || seq == "undefined" {
				seq = "1"
			}
			return seq + ". " + renderElements(block.Ordered.Elements)
		}
	case btTodo:
		if block.Todo != nil {
			check := "[ ]"
			if block.Todo.Style.Done {
				check = "[x]"
			}
			return check + " " + renderElements(block.Todo.Elements)
		}
	case btQuote:
		if block.Quote != nil {
			return renderElements(block.Quote.Elements)
		}
	case btQuoteContainer:
		// Compare by joining child text blocks
		var parts []string
		for _, childID := range block.Children {
			if child, ok := blockMap[childID]; ok {
				rt := getContentRT(child)
				if rt != nil {
					parts = append(parts, renderElements(rt.Elements))
				}
			}
		}
		return strings.Join(parts, "\n")
	case btCode:
		if block.Code != nil {
			var code strings.Builder
			for _, el := range block.Code.Elements {
				if el.TextRun != nil {
					code.WriteString(el.TextRun.Content)
				}
			}
			return fmt.Sprintf("%d\n%s", block.Code.Style.Language, code.String())
		}
	case btDivider:
		return "divider"
	case btImage:
		if block.Image != nil {
			return "image:" + block.Image.Token
		}
	case btFile:
		if block.File != nil {
			return "file:" + block.File.Name + "\t" + block.File.Token
		}
	case btTable:
		var sb strings.Builder
		renderTable(&sb, block, blockMap)
		return strings.TrimSpace(sb.String())
	case btEquation:
		if block.Equation != nil {
			return renderElements(block.Equation.Elements)
		}
	case btCallout:
		// Compare by joining child text blocks
		var parts []string
		for _, childID := range block.Children {
			if child, ok := blockMap[childID]; ok {
				rt := getContentRT(child)
				if rt != nil {
					parts = append(parts, renderElements(rt.Elements))
				}
			}
		}
		return "callout:" + strings.Join(parts, "\n")
	case btLinkPreview:
		if block.LinkPreview != nil {
			return "link:" + block.LinkPreview.URL
		}
	case btGrid:
		return "grid"
	case btDiagram:
		return "diagram"
	case btBoard:
		return "board"
	case btTask:
		return "task"
	default:
		if rt := getHeadingRT(block); rt != nil {
			return renderElements(rt.Elements)
		}
	}
	return ""
}

// buildUpdateRequest builds a batch_update request for blocks that changed content.
func buildUpdateRequest(blockID string, entry mdBlockEntry) map[string]any {
	req := map[string]any{"block_id": blockID}
	elements := entryElements(entry)
	if elements == nil {
		return req
	}
	req["update_text_elements"] = map[string]any{"elements": elements}
	return req
}

func entryElements(entry mdBlockEntry) []any {
	if entry.Body == nil {
		return nil
	}
	switch entry.BlockType {
	case btText:
		if text, ok := entry.Body["text"].(map[string]any); ok {
			if elements, ok := text["elements"].([]any); ok {
				return elements
			}
		}
	case btBullet:
		if bullet, ok := entry.Body["bullet"].(map[string]any); ok {
			if elements, ok := bullet["elements"].([]any); ok {
				return elements
			}
		}
	case btCode:
		if code, ok := entry.Body["code"].(map[string]any); ok {
			if elements, ok := code["elements"].([]any); ok {
				return elements
			}
		}
	default:
		if isHeadingBlockType(entry.BlockType) {
			if h, ok := entry.Body[headingFieldName(entry.BlockType)].(map[string]any); ok {
				if elements, ok := h["elements"].([]any); ok {
					return elements
				}
			}
		}
	}
	return nil
}

func blockTypeName(blockType int) string {
	switch blockType {
	case btText:
		return "text"
	case btHeading1:
		return "heading1"
	case btHeading2:
		return "heading2"
	case btHeading3:
		return "heading3"
	case btHeading4:
		return "heading4"
	case btHeading5:
		return "heading5"
	case btHeading6:
		return "heading6"
	case btHeading7:
		return "heading7"
	case btHeading8:
		return "heading8"
	case btHeading9:
		return "heading9"
	case btBullet:
		return "bullet"
	case btOrdered:
		return "ordered"
	case btCode:
		return "code"
	case btQuote:
		return "quote"
	case btQuoteContainer:
		return "quote_container"
	case btCallout:
		return "callout"
	case btEquation:
		return "equation"
	case btTodo:
		return "todo"
	case btDiagram:
		return "diagram"
	case btDivider:
		return "divider"
	case btFile:
		return "file"
	case btGrid:
		return "grid"
	case btGridColumn:
		return "grid_column"
	case btIframe:
		return "iframe"
	case btImage:
		return "image"
	case btTable:
		return "table"
	case btTableCell:
		return "table_cell"
	case btView:
		return "view"
	case btTask:
		return "task"
	case btBoard:
		return "board"
	case btLinkPreview:
		return "link_preview"
	default:
		return fmt.Sprintf("block_type_%d", blockType)
	}
}

func renderRawElements(elems []any) string {
	var sb strings.Builder
	for _, e := range elems {
		m, ok := e.(map[string]any)
		if !ok {
			continue
		}
		tr, ok := m["text_run"].(map[string]any)
		if !ok {
			continue
		}
		content, _ := tr["content"].(string)
		sb.WriteString(content)
	}
	return sb.String()
}
