package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// Feishu's 文本绘图 ("text diagram") document widget renders Mermaid inline in
// the document body — no whiteboard involved. It is an add-ons block whose
// record carries the source, so the diagram stays editable as text: double-click
// it in Feishu, or rewrite it with these commands.
//
// The widget is the only in-body Mermaid renderer Feishu has: code blocks do not
// render it (lang="mermaid" is not even a recognised code language), and the
// native flowchart/UML block type cannot be created through the API at all.
const (
	diagramWidgetTypeID = "blk_631fefbbae02400430b8f9f4"
	diagramBlockType    = 40 // add_ons
	diagramDefaultView  = "codeChart"
	diagramDefaultTheme = "default"
)

// diagramRecord is the add_ons record — note it travels as a JSON *string*
// inside the block, not as a nested object.
type diagramRecord struct {
	Data  string `json:"data"`
	Theme string `json:"theme"`
	View  string `json:"view"`
}

func diagramBlock(source, theme, view string) (map[string]any, error) {
	record, err := json.Marshal(diagramRecord{Data: source, Theme: theme, View: view})
	if err != nil {
		return nil, fmt.Errorf("encode diagram record: %w", err)
	}
	return map[string]any{
		"block_type": diagramBlockType,
		"add_ons": map[string]any{
			"component_type_id": diagramWidgetTypeID,
			"record":            string(record),
		},
	}, nil
}

// diagramSources returns the Mermaid source of every 文本绘图 widget in a
// blocks response, in document order, paired with its block ID.
func diagramSources(blocks any) [][2]string {
	var parsed struct {
		Items []struct {
			BlockID   string `json:"block_id"`
			BlockType int    `json:"block_type"`
			AddOns    struct {
				ComponentTypeID string `json:"component_type_id"`
				Record          string `json:"record"`
			} `json:"add_ons"`
		} `json:"items"`
	}
	raw, err := json.Marshal(blocks)
	if err != nil {
		return nil
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil
	}
	var out [][2]string
	for _, item := range parsed.Items {
		if item.BlockType != diagramBlockType || item.AddOns.ComponentTypeID != diagramWidgetTypeID {
			continue
		}
		var rec diagramRecord
		if err := json.Unmarshal([]byte(item.AddOns.Record), &rec); err != nil {
			continue
		}
		out = append(out, [2]string{item.BlockID, rec.Data})
	}
	return out
}

// diagramBlockPosition finds a block's parent and its index among that parent's
// children — what a replace needs in order to put the new widget back where the
// old one was, since created blocks otherwise land at the end.
func diagramBlockPosition(blocks any, blockID string) (parentID string, index int, err error) {
	var parsed struct {
		Items []struct {
			BlockID  string   `json:"block_id"`
			ParentID string   `json:"parent_id"`
			Children []string `json:"children"`
		} `json:"items"`
	}
	raw, marshalErr := json.Marshal(blocks)
	if marshalErr != nil {
		return "", 0, marshalErr
	}
	if unmarshalErr := json.Unmarshal(raw, &parsed); unmarshalErr != nil {
		return "", 0, unmarshalErr
	}
	for _, item := range parsed.Items {
		if item.BlockID == blockID {
			parentID = item.ParentID
			break
		}
	}
	if parentID == "" {
		return "", 0, fmt.Errorf("block %s not found in this document", blockID)
	}
	for _, item := range parsed.Items {
		if item.BlockID != parentID {
			continue
		}
		for i, child := range item.Children {
			if child == blockID {
				return parentID, i, nil
			}
		}
	}
	return "", 0, fmt.Errorf("block %s is not listed under its parent %s", blockID, parentID)
}

func readDiagramSource(args []string, argIndex int) (string, error) {
	var r io.Reader = os.Stdin
	if argIndex < len(args) && args[argIndex] != "-" {
		f, err := os.Open(args[argIndex])
		if err != nil {
			return "", fmt.Errorf("open input file: %w", err)
		}
		defer f.Close()
		r = f
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return "", fmt.Errorf("read input: %w", err)
	}
	source := string(data)
	if strings.TrimSpace(source) == "" {
		return "", errors.New("empty diagram source")
	}
	return source, nil
}

func newDocsAddDiagramCmd() *cobra.Command {
	var parentBlockID, theme, view, replaceBlockID string
	var index int

	cmd := &cobra.Command{
		Use:   "add-diagram [document_id_or_url] [file_or_-]",
		Short: "Insert a 文本绘图 widget that renders Mermaid inline in the document body",
		Long: `Insert a Mermaid diagram into the document body as a 文本绘图 widget.

Unlike a whiteboard block, the widget lives in the flow of the text and keeps its
Mermaid source inside the block, so it stays editable as text — in Feishu by
double-clicking it, or here with --replace. Code blocks are not an option:
Feishu has no Mermaid code language, so ` + "`" + "```mermaid" + "`" + ` renders as plain text.

Use a whiteboard (docs add-board) instead when the diagram needs hand layout or
must be dragged around; use this for the ordinary "a flowchart belongs here"
case.

Examples:
  larkctl docs add-diagram TOKEN flow.mmd
  cat state.mmd | larkctl docs add-diagram TOKEN - --index 3
  larkctl docs add-diagram TOKEN flow.mmd --replace doxcnXXX   # rewrite in place`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			source, err := readDiagramSource(args, 1)
			if err != nil {
				return err
			}
			block, err := diagramBlock(source, theme, view)
			if err != nil {
				return err
			}
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			docID, err := resolveDocID(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}

			at := index
			parent := parentBlockID
			if replaceBlockID != "" {
				if cmd.Flags().Changed("index") || cmd.Flags().Changed("block-id") {
					return errors.New("--replace already determines the position; drop --index / --block-id")
				}
				// The widget's record cannot be patched (the block update API
				// has no add_ons field), so replacing means delete then
				// re-create at the same spot.
				blocks, err := client.GetDocumentBlocks(cmd.Context(), docID)
				if err != nil {
					return err
				}
				parent, at, err = diagramBlockPosition(blocks, replaceBlockID)
				if err != nil {
					return err
				}
				if parent == docID {
					parent = "" // document root is addressed as the empty parent
				}
				if _, err := client.DeleteDocumentBlock(cmd.Context(), docID, replaceBlockID); err != nil {
					return fmt.Errorf("remove the old diagram %s: %w", replaceBlockID, err)
				}
			}

			created, err := client.CreateDocumentBlocks(cmd.Context(), docID, parent, map[string]any{
				"index":    at,
				"children": []any{block},
			})
			if err != nil {
				return fmt.Errorf("create diagram block: %w", err)
			}
			blockID, err := diagramBlockIDFromCreate(created)
			if err != nil {
				return err
			}
			fmt.Println(blockID)
			return nil
		},
	}
	cmd.Flags().StringVar(&parentBlockID, "block-id", "", "parent block ID (default: document root)")
	cmd.Flags().IntVar(&index, "index", -1, "insert position among the parent's children (-1 appends)")
	cmd.Flags().StringVar(&replaceBlockID, "replace", "", "rewrite this existing diagram widget in place")
	cmd.Flags().StringVar(&theme, "theme", diagramDefaultTheme, "widget theme")
	cmd.Flags().StringVar(&view, "view", diagramDefaultView, "widget view mode")
	return cmd
}

func diagramBlockIDFromCreate(resp any) (string, error) {
	var parsed struct {
		Children []struct {
			BlockID string `json:"block_id"`
		} `json:"children"`
	}
	raw, err := json.Marshal(resp)
	if err != nil {
		return "", fmt.Errorf("encode create-blocks response: %w", err)
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("parse create-blocks response: %w", err)
	}
	if len(parsed.Children) == 0 || parsed.Children[0].BlockID == "" {
		return "", errors.New("create-blocks returned no block id")
	}
	return parsed.Children[0].BlockID, nil
}

func newDocsDiagramsCmd() *cobra.Command {
	var sourceOnly bool
	cmd := &cobra.Command{
		Use:   "diagrams [document_id_or_url]",
		Short: "List the 文本绘图 widgets in a document and their Mermaid source",
		Long: `List every 文本绘图 widget in a document, with the block ID and the
Mermaid source it renders. Feed a block ID back to 'docs add-diagram --replace'
to rewrite one.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			docID, err := resolveDocID(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			blocks, err := client.GetDocumentBlocks(cmd.Context(), docID)
			if err != nil {
				return err
			}
			found := diagramSources(blocks)
			if len(found) == 0 {
				return errors.New("no 文本绘图 widget in this document")
			}
			if sourceOnly {
				for _, d := range found {
					fmt.Println(d[1])
				}
				return nil
			}
			out := make([]map[string]string, 0, len(found))
			for _, d := range found {
				out = append(out, map[string]string{"block_id": d[0], "source": d[1]})
			}
			printJSON(map[string]any{"diagrams": out})
			return nil
		},
	}
	cmd.Flags().BoolVar(&sourceOnly, "source", false, "print only the Mermaid source, one diagram after another")
	return cmd
}
