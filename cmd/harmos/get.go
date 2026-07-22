package main

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/pottom/harmos/internal/search"
)

func newGetCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "get <query>",
		Short: "Print the matching password to stdout; refuses to guess when ambiguous",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGet(configPath, args[0], cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "config file (default: $XDG_CONFIG_HOME/harmos/config.toml)")
	return cmd
}

func runGet(configPath, query string, out io.Writer) error {
	res, err := openAll(configPath)
	if err != nil {
		return err
	}
	warnExcluded(res)

	hits := search.New(res.Entries).Match(query)
	switch {
	case len(hits) == 0:
		return fmt.Errorf("no entry matches %q", query)

	case len(hits) == 1 || hits[0].Score < hits[1].Score:
		// a unique best — safe to return. Password to stdout; provenance to
		// stderr so `pw=$(harmos get x)` gets only the secret.
		best := hits[0].Entry
		emitf(out, "%s\n", best.Password.Reveal())
		fmt.Fprintf(os.Stderr, "%s · %s · %s\n", best.Source, best.Path, best.Username)
		return nil

	default:
		// ambiguous — never guess in a scriptable command (spec §8b).
		fmt.Fprintf(os.Stderr, "ambiguous: %q matches several entries — qualify it:\n", query)
		for _, r := range hits {
			if r.Score > hits[0].Score {
				break
			}
			fmt.Fprintf(os.Stderr, "  %s · %s · %s\n", r.Entry.Source, r.Entry.Path, r.Entry.Username)
		}
		return fmt.Errorf("refusing to guess; qualify the query")
	}
}
