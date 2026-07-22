package main

import (
	"io"
	"sort"

	"github.com/spf13/cobra"

	"github.com/pottom/harmos/internal/vault"
)

func newLsCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "ls [profile]",
		Short: "List entries as tab-separated columns (scriptable, no TTY needed)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			profile := ""
			if len(args) == 1 {
				profile = args[0]
			}
			return runLs(configPath, profile, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "config file (default: $XDG_CONFIG_HOME/harmos/config.toml)")
	return cmd
}

func runLs(configPath, profile string, out io.Writer) error {
	res, _, err := openAll(configPath)
	if err != nil {
		return err
	}

	entries := res.Entries
	if profile != "" {
		var filtered []vault.Entry
		for _, e := range entries {
			if e.Source == profile {
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

	for _, e := range entries {
		emitf(out, "%s\t%s\t%s\t%s\n", e.Source, e.Path, e.Title, e.Username)
	}
	warnExcluded(res)
	return nil
}
