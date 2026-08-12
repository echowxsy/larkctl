package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var outputFormat string

type commandSchema struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Usage       string          `json:"usage,omitempty"`
	Flags       []flagSchema    `json:"flags,omitempty"`
	Subcommands []commandSchema `json:"subcommands,omitempty"`
}

type flagSchema struct {
	Name        string `json:"name"`
	Shorthand   string `json:"shorthand,omitempty"`
	Type        string `json:"type"`
	Default     string `json:"default,omitempty"`
	Description string `json:"description"`
}

func buildSchema(cmd *cobra.Command) commandSchema {
	cs := commandSchema{
		Name:        cmd.Name(),
		Description: cmd.Short,
		Usage:       cmd.UseLine(),
	}
	cmd.LocalFlags().VisitAll(func(f *pflag.Flag) {
		if f.Hidden || f.Name == "help" {
			return
		}
		cs.Flags = append(cs.Flags, flagSchema{
			Name:        f.Name,
			Shorthand:   f.Shorthand,
			Type:        f.Value.Type(),
			Default:     f.DefValue,
			Description: f.Usage,
		})
	})
	for _, sub := range cmd.Commands() {
		if sub.Hidden || sub.Name() == "help" || sub.Name() == "completion" {
			continue
		}
		cs.Subcommands = append(cs.Subcommands, buildSchema(sub))
	}
	return cs
}

func printSchemaJSON(schema commandSchema) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(schema)
}

func printSchemaTable(schema commandSchema) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	printSchemaRecurse(tw, schema, true)
	tw.Flush()
}

func printSchemaRecurse(tw *tabwriter.Writer, schema commandSchema, isRoot bool) {
	if isRoot {
		// Separate root-level commands into groups (have subcommands) and leaves
		var groups []commandSchema
		var leaves []commandSchema
		for _, sub := range schema.Subcommands {
			if len(sub.Subcommands) > 0 {
				groups = append(groups, sub)
			} else {
				leaves = append(leaves, sub)
			}
		}
		// Print groups first
		for _, g := range groups {
			fmt.Fprintf(tw, "\n%s:\n", g.Name)
			for _, sub := range g.Subcommands {
				printSchemaLeaf(tw, sub)
			}
		}
		// Print root-level leaf commands
		if len(leaves) > 0 {
			fmt.Fprintf(tw, "\ncommands:\n")
			for _, l := range leaves {
				printSchemaLeaf(tw, l)
			}
		}
	}
}

func printSchemaLeaf(tw *tabwriter.Writer, schema commandSchema) {
	flags := make([]string, 0)
	for _, f := range schema.Flags {
		flags = append(flags, "--"+f.Name)
	}
	flagStr := ""
	if len(flags) > 0 {
		flagStr = strings.Join(flags, ", ")
	}
	fmt.Fprintf(tw, "  %s\t%s\t%s\n", schema.Name, schema.Description, flagStr)
}

func newSchemaCmd(rootCmd *cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "schema [command...]",
		Short: "Show command schema (all commands, flags, and usage)",
		Long: `Inspect larkctl command schema.

Examples:
  larkctl schema                    # all commands (table view)
  larkctl schema --format json      # full schema as JSON
  larkctl schema im                 # IM commands only
  larkctl schema tasks create       # specific command`,
		RunE: func(cmd *cobra.Command, args []string) error {
			target := rootCmd
			for _, a := range args {
				found := false
				for _, sub := range target.Commands() {
					if sub.Name() == a {
						target = sub
						found = true
						break
					}
				}
				if !found {
					return fmt.Errorf("unknown command: %s", a)
				}
			}
			schema := buildSchema(target)
			if outputFormat == "json" {
				printSchemaJSON(schema)
			} else {
				printSchemaTable(schema)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&outputFormat, "format", "", "output format: table (default) or json")
	return cmd
}
