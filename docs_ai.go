package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// resolveContentArg expands the @file and - (stdin) forms of a content flag,
// and passes anything else through as a literal.
func resolveContentArg(arg string) (string, error) {
	switch {
	case arg == "-":
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		return string(data), nil
	case strings.HasPrefix(arg, "@"):
		data, err := os.ReadFile(strings.TrimPrefix(arg, "@"))
		if err != nil {
			return "", fmt.Errorf("read content file: %w", err)
		}
		return string(data), nil
	default:
		return arg, nil
	}
}

// The docs_ai commands wrap Feishu's higher-level document API. The classic
// block endpoints (docs create-blocks / delete-block / update) stay as they
// are: they address individual blocks by ID, which is what the Markdown diff
// flow needs. These commands exist for what that API cannot express —
// tables of any size, moving or copying blocks, replacing a range, and reading
// only part of a document.

// docsEditCommands are the server-side edit verbs, with the flags each one
// needs. Kept in one place so the help text and the validation cannot drift.
var docsEditCommands = map[string]string{
	"str_replace":             "--pattern (plus --content; empty --content deletes the match)",
	"block_delete":            "--block-id, or --start-block-id with --end-block-id",
	"block_insert_after":      "--block-id and --content",
	"block_replace":           "--content, plus --block-id or --start-block-id/--end-block-id",
	"block_copy_insert_after": "--block-id and --src-block-ids",
	"block_move_after":        "--block-id and --src-block-ids",
	"append":                  "--content",
	"overwrite":               "--content (replaces the whole document)",
}

func docsEditCommandList() string {
	names := make([]string, 0, len(docsEditCommands))
	for name := range docsEditCommands {
		names = append(names, name)
	}
	// Stable help text beats map iteration order.
	order := []string{"str_replace", "block_insert_after", "block_replace", "block_delete",
		"block_move_after", "block_copy_insert_after", "append", "overwrite"}
	sorted := make([]string, 0, len(names))
	for _, name := range order {
		if _, ok := docsEditCommands[name]; ok {
			sorted = append(sorted, name)
		}
	}
	return strings.Join(sorted, " | ")
}

func newDocsEditCmd() *cobra.Command {
	var command, content, contentFormat string
	var blockID, srcBlockIDs, pattern, startBlockID, endBlockID string
	var revisionID int

	cmd := &cobra.Command{
		Use:   "edit [document_id_or_url]",
		Short: "Edit a document with one server-side command (move/copy/replace blocks, tables, ...)",
		Long: `Run one edit command against a document.

Feishu applies the edit server-side, so this reaches what block-level editing
cannot: DocxXML content may contain tables of any size (the block API caps a new
table at 9 rows), blocks can be moved or copied, and a sibling range can be
replaced or deleted in one call.

Commands and the flags they need:
  str_replace              ` + docsEditCommands["str_replace"] + `
  block_insert_after       ` + docsEditCommands["block_insert_after"] + `
  block_replace            ` + docsEditCommands["block_replace"] + `
  block_delete             ` + docsEditCommands["block_delete"] + `
  block_move_after         ` + docsEditCommands["block_move_after"] + `
  block_copy_insert_after  ` + docsEditCommands["block_copy_insert_after"] + `
  append                   ` + docsEditCommands["append"] + `
  overwrite                ` + docsEditCommands["overwrite"] + `

Block IDs come from 'docs fetch --detail with-ids'; refetch after a structural
edit before reusing them. --content takes @file or - for stdin.

Examples:
  larkctl docs edit TOKEN --command append --content @table.xml
  larkctl docs edit TOKEN --command str_replace --pattern "job_finished" --content "job_succeeded"
  larkctl docs edit TOKEN --command block_move_after --block-id doxcnAAA --src-block-ids doxcnBBB`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, ok := docsEditCommands[command]; !ok {
				return fmt.Errorf("unknown --command %q (want one of: %s)", command, docsEditCommandList())
			}
			if contentFormat != "xml" && contentFormat != "markdown" {
				return fmt.Errorf("unknown --format %q (want xml or markdown)", contentFormat)
			}
			if startBlockID != "" && endBlockID == "" || endBlockID != "" && startBlockID == "" {
				return errors.New("--start-block-id and --end-block-id must be used together")
			}
			if blockID != "" && startBlockID != "" {
				return errors.New("--block-id and --start-block-id/--end-block-id are mutually exclusive")
			}

			body := map[string]any{
				"command":     command,
				"format":      contentFormat,
				"revision_id": revisionID,
			}
			// Read @file / - only when content is actually wanted: block_delete
			// and the move/copy commands carry none.
			if content != "" {
				resolved, err := resolveContentArg(content)
				if err != nil {
					return err
				}
				body["content"] = resolved
			} else if cmd.Flags().Changed("content") {
				// An explicitly empty --content is meaningful: with str_replace
				// it deletes the match.
				body["content"] = ""
			}
			for key, value := range map[string]string{
				"block_id":       blockID,
				"src_block_ids":  srcBlockIDs,
				"pattern":        pattern,
				"start_block_id": startBlockID,
				"end_block_id":   endBlockID,
			} {
				if value != "" {
					body[key] = value
				}
			}

			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			data, err := client.EditDocument(cmd.Context(), args[0], body)
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}
	cmd.Flags().StringVar(&command, "command", "", "edit command: "+docsEditCommandList())
	cmd.Flags().StringVar(&content, "content", "", "content to insert or replace with (@file or - for stdin)")
	cmd.Flags().StringVar(&contentFormat, "format", "xml", "content format: xml (DocxXML) or markdown")
	cmd.Flags().StringVar(&blockID, "block-id", "", "target block ID")
	cmd.Flags().StringVar(&srcBlockIDs, "src-block-ids", "", "comma-separated source block IDs to move or copy")
	cmd.Flags().StringVar(&pattern, "pattern", "", "text matched by str_replace")
	cmd.Flags().StringVar(&startBlockID, "start-block-id", "", "inclusive range start (with --end-block-id)")
	cmd.Flags().StringVar(&endBlockID, "end-block-id", "", "inclusive range end (with --start-block-id)")
	cmd.Flags().IntVar(&revisionID, "revision-id", -1, "base revision for optimistic concurrency (-1 is latest)")
	_ = cmd.MarkFlagRequired("command")
	return cmd
}

func newDocsFetchCmd() *cobra.Command {
	var scope, keyword, detail, docFormat string
	var startBlockID, endBlockID string
	var maxDepth, contextBefore, contextAfter int

	cmd := &cobra.Command{
		Use:   "fetch [document_id_or_url]",
		Short: "Read a document — whole, outline only, one section, a block range, or by keyword",
		Long: `Read a document without pulling every block.

'docs blocks' returns the raw block tree and 'docs export' the whole file; this
reads the part you name, which is what makes editing large documents practical:

  --scope full      whole document (default)
  --scope outline   headings only, capped by --max-depth
  --scope section   the subtree under --start-block-id
  --scope range     --start-block-id .. --end-block-id
  --scope keyword   blocks matching --keyword ('|' separates OR branches)

--detail with-ids adds the block IDs that 'docs edit' needs; --detail full also
returns styles and edit metadata.

Examples:
  larkctl docs fetch TOKEN --scope outline
  larkctl docs fetch TOKEN --scope keyword --keyword "job_canceled|job_failed" --detail with-ids
  larkctl docs fetch TOKEN --scope section --start-block-id doxcnAAA --format markdown`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			readModes := map[string]bool{"full": true, "outline": true, "section": true, "range": true, "keyword": true}
			if !readModes[scope] {
				return fmt.Errorf("unknown --scope %q (want full, outline, section, range or keyword)", scope)
			}
			if docFormat != "xml" && docFormat != "markdown" {
				return fmt.Errorf("unknown --format %q (want xml or markdown)", docFormat)
			}
			switch scope {
			case "keyword":
				if keyword == "" {
					return errors.New("--scope keyword needs --keyword")
				}
			case "section":
				if startBlockID == "" {
					return errors.New("--scope section needs --start-block-id")
				}
			case "range":
				if startBlockID == "" && endBlockID == "" {
					return errors.New("--scope range needs --start-block-id and/or --end-block-id")
				}
			}

			body := map[string]any{
				"format": docFormat,
				"export_option": map[string]any{
					"export_block_id":        detail == "with-ids" || detail == "full",
					"export_style_attrs":     detail == "full",
					"export_cite_extra_data": detail == "full",
				},
				"extra_param": `{"include_comments":true}`,
			}
			if scope != "full" {
				// The API takes these as strings, not numbers.
				readOption := map[string]any{"read_mode": scope}
				for key, value := range map[string]string{
					"keyword":        keyword,
					"start_block_id": startBlockID,
					"end_block_id":   endBlockID,
				} {
					if value != "" {
						readOption[key] = value
					}
				}
				if maxDepth > 0 {
					readOption["max_depth"] = strconv.Itoa(maxDepth)
				}
				if contextBefore > 0 {
					readOption["context_before"] = strconv.Itoa(contextBefore)
				}
				if contextAfter > 0 {
					readOption["context_after"] = strconv.Itoa(contextAfter)
				}
				body["read_option"] = readOption
			}

			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			data, err := client.FetchDocument(cmd.Context(), args[0], body)
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}
	cmd.Flags().StringVar(&scope, "scope", "full", "read scope: full, outline, section, range or keyword")
	cmd.Flags().StringVar(&keyword, "keyword", "", "keyword for --scope keyword ('|' separates OR branches)")
	cmd.Flags().StringVar(&detail, "detail", "simple", "detail level: simple, with-ids or full")
	cmd.Flags().StringVar(&docFormat, "format", "xml", "output format: xml (DocxXML) or markdown")
	cmd.Flags().StringVar(&startBlockID, "start-block-id", "", "anchor block for --scope section/range")
	cmd.Flags().StringVar(&endBlockID, "end-block-id", "", "range end block for --scope range")
	cmd.Flags().IntVar(&maxDepth, "max-depth", 0, "heading depth cap for --scope outline")
	cmd.Flags().IntVar(&contextBefore, "context-before", 0, "sibling blocks to include before the selection")
	cmd.Flags().IntVar(&contextAfter, "context-after", 0, "sibling blocks to include after the selection")
	return cmd
}

func newDocsHistoryCmd() *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "history [document_id_or_url]",
		Short: "List a document's history versions",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			data, err := client.ListDocumentHistories(cmd.Context(), args[0], limit)
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 20, "number of versions to return")
	return cmd
}

func newDocsRevertCmd() *cobra.Command {
	var versionID string
	var waitMS int
	cmd := &cobra.Command{
		Use:   "revert [document_id_or_url]",
		Short: "Revert a document to a history version",
		Long: `Revert a document to one of its history versions.

The version ID comes from 'docs history'. The revert runs as a task: if it does
not finish within --wait-ms the response carries a task_id to poll with
'docs revert-status'.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			body := map[string]any{
				"history_version_id": versionID,
				"wait_timeout_ms":    waitMS,
			}
			data, err := client.RevertDocument(cmd.Context(), args[0], body)
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}
	cmd.Flags().StringVar(&versionID, "version", "", "history_version_id from 'docs history'")
	cmd.Flags().IntVar(&waitMS, "wait-ms", 30000, "how long to wait for the revert before returning a task_id")
	_ = cmd.MarkFlagRequired("version")
	return cmd
}

func newDocsRevertStatusCmd() *cobra.Command {
	var taskID string
	cmd := &cobra.Command{
		Use:   "revert-status [document_id_or_url]",
		Short: "Poll a document revert task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			data, err := client.GetDocumentRevertStatus(cmd.Context(), args[0], taskID)
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}
	cmd.Flags().StringVar(&taskID, "task-id", "", "task_id returned by 'docs revert'")
	_ = cmd.MarkFlagRequired("task-id")
	return cmd
}
