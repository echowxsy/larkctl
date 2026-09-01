package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// docsMediaKind captures the block wiring for the two kinds of media a document
// block can hold. Both follow the same three-step dance: create an empty block,
// upload the bytes bound to that block, then patch the returned token into it.
type docsMediaKind struct {
	blockType  int    // 27 (image) or 23 (file)
	field      string // "image" | "file" — payload key of the empty block
	parentType string // "docx_image" | "docx_file" — media upload parent_type
	replaceKey string // "replace_image" | "replace_file" — batch_update key
}

var (
	docsImageKind = docsMediaKind{blockType: 27, field: "image", parentType: "docx_image", replaceKey: "replace_image"}
	docsFileKind  = docsMediaKind{blockType: 23, field: "file", parentType: "docx_file", replaceKey: "replace_file"}
)

// docURLObjTypes maps the path segment of a Feishu cloud-document URL to the
// obj_type a mention_doc element needs. The value matters: a wiki token sent
// with obj_type 22 comes back as "resource not found".
var docURLObjTypes = map[string]int{
	"docx":      22,
	"docs":      1,
	"wiki":      16,
	"sheets":    3,
	"base":      8,
	"file":      12,
	"mindnotes": 11,
	"slides":    15,
}

const defaultMentionObjType = 22 // docx

// resolveDocID turns a document ID, a docx URL or a wiki URL into the document
// ID that the docx block APIs accept. Wiki tokens are rejected by those APIs,
// so the node lookup failing is fatal rather than best-effort.
func resolveDocID(ctx context.Context, client FeishuClient, input string) (string, error) {
	docID := extractToken(input)
	if !strings.Contains(input, "/wiki/") {
		return docID, nil
	}
	data, err := client.GetWikiNode(ctx, input)
	if err != nil {
		return "", fmt.Errorf("resolve wiki token: %w", err)
	}
	if m, ok := data.(map[string]any); ok {
		if node, ok := m["node"].(map[string]any); ok {
			if objToken, ok := node["obj_token"].(string); ok && objToken != "" {
				return objToken, nil
			}
		}
	}
	return "", fmt.Errorf("wiki node %s has no obj_token", docID)
}

// mediaBlockIDFromCreate reports both IDs a create-blocks response yields: the
// block that was added to the parent, and the block that actually holds the
// media token. They differ for attachments — Feishu answers a file-block request
// with a view block (33) wrapping the real file block (23), so the token goes to
// the child while the wrapper is what has to be removed on failure.
func mediaBlockIDFromCreate(resp any, wantType int) (createdID, mediaID string, err error) {
	var parsed struct {
		Children []struct {
			BlockID   string   `json:"block_id"`
			BlockType int      `json:"block_type"`
			Children  []string `json:"children"`
		} `json:"children"`
	}
	b, err := json.Marshal(resp)
	if err != nil {
		return "", "", fmt.Errorf("encode create-blocks response: %w", err)
	}
	if err := json.Unmarshal(b, &parsed); err != nil {
		return "", "", fmt.Errorf("parse create-blocks response: %w", err)
	}
	if len(parsed.Children) == 0 {
		return "", "", fmt.Errorf("create-blocks returned no children")
	}
	child := parsed.Children[0]
	if child.BlockType == wantType {
		return child.BlockID, child.BlockID, nil
	}
	if len(child.Children) > 0 {
		return child.BlockID, child.Children[0], nil
	}
	return "", "", fmt.Errorf("create-blocks returned block_type=%d with no block_type=%d child", child.BlockType, wantType)
}

// addDocsMedia uploads path into docID and returns the ID of the block holding it.
func addDocsMedia(ctx context.Context, client FeishuClient, docID, parentBlockID string, index int, path string, kind docsMediaKind) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory", path)
	}

	created, err := client.CreateDocumentBlocks(ctx, docID, parentBlockID, map[string]any{
		"index": index,
		"children": []any{map[string]any{
			"block_type": kind.blockType,
			kind.field:   map[string]any{"token": ""},
		}},
	})
	if err != nil {
		return "", fmt.Errorf("create %s block: %w", kind.field, err)
	}
	createdID, blockID, err := mediaBlockIDFromCreate(created, kind.blockType)
	if err != nil {
		return "", err
	}

	// Anything after this point leaves a tokenless block behind, which Feishu
	// renders as a broken "failed to load" placeholder. Roll it back so a failed
	// upload does not scar the document.
	rollback := func(cause error) error {
		if _, delErr := client.DeleteDocumentBlock(ctx, docID, createdID); delErr != nil {
			return fmt.Errorf("%w (also failed to remove the empty %s block %s: %v)", cause, kind.field, createdID, delErr)
		}
		return cause
	}

	f, err := os.Open(path)
	if err != nil {
		return "", rollback(err)
	}
	defer f.Close()

	fileToken, err := client.UploadDocsMedia(ctx, kind.parentType, blockID, filepath.Base(path), f, info.Size())
	if err != nil {
		return "", rollback(fmt.Errorf("upload media: %w", err))
	}

	_, err = client.UpdateDocumentBlocks(ctx, docID, map[string]any{
		"requests": []any{map[string]any{
			"block_id":      blockID,
			kind.replaceKey: map[string]any{"token": fileToken},
		}},
	})
	if err != nil {
		return "", rollback(fmt.Errorf("attach media to block %s: %w", blockID, err))
	}
	return blockID, nil
}

func newDocsAddMediaCmd(use, short, long string, kind docsMediaKind) *cobra.Command {
	var parentBlockID string
	var index int
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Long:  long,
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			docID, err := resolveDocID(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			at := index
			for _, path := range args[1:] {
				blockID, err := addDocsMedia(cmd.Context(), client, docID, parentBlockID, at, path, kind)
				if err != nil {
					return fmt.Errorf("%s: %w", path, err)
				}
				fmt.Printf("%s -> %s\n", path, blockID)
				if at >= 0 {
					at++ // keep the argument order instead of reversing it
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&parentBlockID, "block-id", "", "parent block ID (default: document root)")
	cmd.Flags().IntVar(&index, "index", -1, "insert position among the parent's children (-1 appends)")
	return cmd
}

func newDocsAddImageCmd() *cobra.Command {
	return newDocsAddMediaCmd(
		"add-image [document_id_or_url] [image_path...]",
		"Upload local images and insert them as image blocks",
		`Upload local image files and insert them into a document as image blocks.

Feishu sizes the block from the image itself; there is no separate width/height
to pass. Max 20MB per file.

Examples:
  larkctl docs add-image https://xxx.feishu.cn/docx/TOKEN chart.png
  larkctl docs add-image TOKEN a.png b.png --index 3
  larkctl docs add-image TOKEN a.png --block-id doxcnXXX   # into a callout/table cell`,
		docsImageKind)
}

func newDocsAddFileCmd() *cobra.Command {
	return newDocsAddMediaCmd(
		"add-file [document_id_or_url] [file_path...]",
		"Upload local files and insert them as attachment blocks",
		`Upload local files and insert them into a document as file attachment blocks.

Feishu wraps each attachment in a view block; the printed block ID is the inner
file block, which is what "docs files" downloads. Max 20MB per file.

Examples:
  larkctl docs add-file https://xxx.feishu.cn/docx/TOKEN report.pdf
  larkctl docs add-file TOKEN a.zip b.xlsx`,
		docsFileKind)
}

func newDocsAddLinkCmd() *cobra.Command {
	var text, parentBlockID string
	var index int
	cmd := &cobra.Command{
		Use:   "add-link [document_id_or_url] [url]",
		Short: "Append a paragraph containing a hyperlink",
		Long: `Append a paragraph whose text links to the given URL.

Examples:
  larkctl docs add-link TOKEN https://example.com
  larkctl docs add-link TOKEN https://example.com --text "Design spec"`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			target := args[1]
			if err := validateLinkURL(target); err != nil {
				return fmt.Errorf("invalid link URL %q: %w", target, err)
			}
			anchor := text
			if anchor == "" {
				anchor = target
			}
			docID, err := resolveDocID(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			element := map[string]any{"text_run": map[string]any{
				"content": anchor,
				"text_element_style": map[string]any{
					"link": map[string]any{"url": url.QueryEscape(target)},
				},
			}}
			return createDocsTextBlock(cmd.Context(), client, docID, parentBlockID, index, []any{element})
		},
	}
	cmd.Flags().StringVar(&text, "text", "", "link text (default: the URL itself)")
	cmd.Flags().StringVar(&parentBlockID, "block-id", "", "parent block ID (default: document root)")
	cmd.Flags().IntVar(&index, "index", -1, "insert position among the parent's children (-1 appends)")
	return cmd
}

func newDocsAddMentionCmd() *cobra.Command {
	var text, parentBlockID string
	var index, objType int
	cmd := &cobra.Command{
		Use:   "add-mention [document_id_or_url] [target...]",
		Short: "Append a paragraph mentioning documents (@doc) or people (@user)",
		Long: `Append a paragraph containing inline mentions — the chips Feishu shows for a
linked document or an @-ed person.

Each target is resolved by shape:
  - a cloud-document URL   -> document mention, obj_type inferred from the path
                              (/docx/ /docs/ /wiki/ /sheets/ /base/ /file/ ...)
  - a bare document token  -> document mention with --obj-type (default 22, docx)
  - ou_... / on_...        -> person mention by open_id / union_id
  - anything else          -> looked up as a person name (must match exactly one)

Examples:
  larkctl docs add-mention TOKEN https://xxx.feishu.cn/wiki/WIKITOKEN
  larkctl docs add-mention TOKEN 张三 --text "Owner: "
  larkctl docs add-mention TOKEN ou_f1ce... https://xxx.feishu.cn/docx/OTHER`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			docID, err := resolveDocID(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}

			var elements []any
			if text != "" {
				elements = append(elements, map[string]any{"text_run": map[string]any{"content": text}})
			}
			for i, target := range args[1:] {
				if i > 0 {
					elements = append(elements, map[string]any{"text_run": map[string]any{"content": " "}})
				}
				el, err := docsMentionElement(cmd.Context(), client, target, objType)
				if err != nil {
					return err
				}
				elements = append(elements, el)
			}
			return createDocsTextBlock(cmd.Context(), client, docID, parentBlockID, index, elements)
		},
	}
	cmd.Flags().StringVar(&text, "text", "", "text placed before the mentions")
	cmd.Flags().IntVar(&objType, "obj-type", defaultMentionObjType, "obj_type for bare document tokens (22 docx, 16 wiki, 3 sheet, 8 bitable, 1 doc, 11 mindnote, 12 file, 15 slide)")
	cmd.Flags().StringVar(&parentBlockID, "block-id", "", "parent block ID (default: document root)")
	cmd.Flags().IntVar(&index, "index", -1, "insert position among the parent's children (-1 appends)")
	return cmd
}

// docsMentionElement builds one inline mention element from a user-supplied target.
func docsMentionElement(ctx context.Context, client FeishuClient, target string, defaultObjType int) (map[string]any, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return nil, fmt.Errorf("empty mention target")
	}

	if strings.HasPrefix(target, "ou_") || strings.HasPrefix(target, "on_") {
		return map[string]any{"mention_user": map[string]any{"user_id": target}}, nil
	}

	if objType, ok := docMentionObjTypeFromURL(target); ok {
		token := extractToken(target)
		if token == "" {
			return nil, fmt.Errorf("cannot extract a document token from %q", target)
		}
		return map[string]any{"mention_doc": map[string]any{"token": token, "obj_type": objType}}, nil
	}

	if looksLikeDocToken(target) {
		return map[string]any{"mention_doc": map[string]any{"token": target, "obj_type": defaultObjType}}, nil
	}

	openID, err := resolveUserOpenID(ctx, client, target)
	if err != nil {
		return nil, err
	}
	return map[string]any{"mention_user": map[string]any{"user_id": openID}}, nil
}

// docMentionObjTypeFromURL reports the mention obj_type for a Feishu cloud-document
// URL. The second result is false when the target is not a URL at all.
func docMentionObjTypeFromURL(target string) (int, bool) {
	if !strings.Contains(target, "://") && !strings.Contains(target, "/") {
		return 0, false
	}
	parsed, err := url.Parse(target)
	if err != nil {
		return 0, false
	}
	for _, segment := range strings.Split(parsed.Path, "/") {
		if objType, ok := docURLObjTypes[segment]; ok {
			return objType, true
		}
	}
	return defaultMentionObjType, true
}

// looksLikeDocToken reports whether target has the shape of a bare cloud-document
// token, so it is not sent to the contact search as if it were a person's name.
func looksLikeDocToken(target string) bool {
	if len(target) < 20 {
		return false
	}
	for _, r := range target {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// resolveUserOpenID looks a person up by name and insists on a single match, so
// a mention never silently points at the wrong colleague.
func resolveUserOpenID(ctx context.Context, client FeishuClient, name string) (string, error) {
	if err := requireScopes(ctx, client, contactSearchScopes); err != nil {
		return "", err
	}
	data, err := client.SearchUsers(ctx, name, 20)
	if err != nil {
		return "", fmt.Errorf("look up %q: %w", name, err)
	}
	var parsed struct {
		Users []struct {
			Name   string `json:"name"`
			OpenID string `json:"open_id"`
		} `json:"users"`
	}
	b, _ := json.Marshal(data)
	if err := json.Unmarshal(b, &parsed); err != nil {
		return "", fmt.Errorf("parse user search response: %w", err)
	}

	var matches []string
	var names []string
	for _, u := range parsed.Users {
		if u.OpenID == "" {
			continue
		}
		names = append(names, u.Name)
		if u.Name == name {
			matches = append(matches, u.OpenID)
		}
	}
	switch {
	case len(matches) == 1:
		return matches[0], nil
	case len(matches) > 1:
		return "", fmt.Errorf("%q matches %d people; pass an ou_... open_id instead (see `larkctl im find`)", name, len(matches))
	case len(names) == 0:
		return "", fmt.Errorf("no user named %q", name)
	default:
		return "", fmt.Errorf("no exact match for %q; closest: %s (use `larkctl im find` for open_ids)", name, strings.Join(names, ", "))
	}
}

// createDocsTextBlock appends a paragraph built from ready-made inline elements.
func createDocsTextBlock(ctx context.Context, client FeishuClient, docID, parentBlockID string, index int, elements []any) error {
	data, err := client.CreateDocumentBlocks(ctx, docID, parentBlockID, map[string]any{
		"index": index,
		"children": []any{map[string]any{
			"block_type": 2,
			"text":       map[string]any{"elements": elements, "style": map[string]any{}},
		}},
	})
	if err != nil {
		return err
	}
	printJSON(data)
	return nil
}
