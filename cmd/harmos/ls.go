package main

import (
	"io"
	"sort"

	"github.com/spf13/cobra"

	"github.com/pottom/harmos/internal/vault"
)

func newLsCmd() *cobra.Command {
	var configPath string
	var noHeaders bool
	cmd := &cobra.Command{
		Use:   "ls [source]",
		Short: "List entries in an aligned table (no TTY needed)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			source := ""
			if len(args) == 1 {
				source = args[0]
			}
			return runLs(configPath, source, !noHeaders, cmd.OutOrStdout())
		},
	}
	cmd.Flags().BoolVar(&noHeaders, "no-headers", false, "omit the header row (for scripting)")
	cmd.Flags().StringVar(&configPath, "config", "", "config file (default: $XDG_CONFIG_HOME/harmos/config.toml)")
	return cmd
}

func runLs(configPath, source string, showHeaders bool, out io.Writer) error {
	res, _, err := openAll(configPath)
	if err != nil {
		return err
	}

	entries := res.Entries
	if source != "" {
		var filtered []vault.Entry
		for _, e := range entries {
			if e.Source == source {
				filtered = append(filtered, e)
			}
		}
		entries = filtered
	}
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return a.Title < b.Title
	})

	rows := make([][]string, 0, len(entries))
	for _, e := range entries {
		rows = append(rows, []string{e.Source, e.Path, e.Title, e.Username})
	}
	printTable(out, []string{"SOURCE", "PATH", "TITLE", "USERNAME"}, rows, showHeaders)
	warnExcluded(res)
	return nil
}
