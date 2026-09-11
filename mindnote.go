package main

import (
	"github.com/spf13/cobra"
)

// Mind notes (思维笔记) are their own document type, not a docx block, with a
// flat node API: read the whole tree, or post nodes to create and update them.
// Creating the mind note file itself is not an open API — make it in Feishu (or
// copy one) and pass its token here.
func newMindnoteCmd() *cobra.Command {
	mindnoteCmd := &cobra.Command{
		Use:   "mindnote",
		Short: "Feishu mind note (思维笔记) commands",
	}

	nodesCmd := &cobra.Command{
		Use:   "nodes [mindnote_id_or_url]",
		Short: "Get all nodes of a mind note",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			data, err := client.GetMindnoteNodes(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}

	createNodesCmd := &cobra.Command{
		Use:   "create-nodes [mindnote_id_or_url] [json_file_or_-]",
		Short: "Create or update mind note nodes from JSON ({\"nodes\":[...]})",
		Long: `Create or update nodes on a mind note.

The JSON mirrors the Feishu mindnote API — read an existing mind note with
'mindnote nodes' and follow the shape it returns.

Examples:
  larkctl mindnote nodes MINDNOTE_TOKEN
  larkctl mindnote create-nodes MINDNOTE_TOKEN nodes.json
  cat nodes.json | larkctl mindnote create-nodes MINDNOTE_TOKEN -`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			var body map[string]any
			if err := readJSONInput(args, 1, &body); err != nil {
				return err
			}
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			if err := requireScopes(cmd.Context(), client, "mindnote:node:create"); err != nil {
				return err
			}
			data, err := client.CreateMindnoteNodes(cmd.Context(), args[0], body)
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}

	mindnoteCmd.AddCommand(nodesCmd)
	mindnoteCmd.AddCommand(createNodesCmd)
	return mindnoteCmd
}
