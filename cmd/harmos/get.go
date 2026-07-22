package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	"github.com/spf13/cobra"

	"github.com/pottom/harmos/internal/clip"
	"github.com/pottom/harmos/internal/search"
	"github.com/pottom/harmos/internal/vault"
)

func newGetCmd() *cobra.Command {
	var configPath string
	var doCopy bool
	cmd := &cobra.Command{
		Use:   "get <query>",
		Short: "Print (or --copy) the matching password; refuses to guess when ambiguous",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGet(configPath, args[0], doCopy, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "config file (default: $XDG_CONFIG_HOME/harmos/config.toml)")
	cmd.Flags().BoolVar(&doCopy, "copy", false, "copy to the clipboard (concealed), auto-cleared after the timeout")
	return cmd
}

func runGet(configPath, query string, doCopy bool, out io.Writer) error {
	res, cfg, err := openAll(configPath)
	if err != nil {
		return err
	}
	warnExcluded(res)

	hits := search.New(res.Entries).Match(query)
	switch {
	case len(hits) == 0:
		return fmt.Errorf("no entry matches %q", query)

	case len(hits) == 1 || hits[0].Score < hits[1].Score:
		// a unique best — safe to return.
		best := hits[0].Entry
		if doCopy {
			return copyPassword(best, cfg.ClipboardTimeout.Duration)
		}
		// password → stdout, provenance → stderr, so pw=$(harmos get x) is clean.
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

// copyPassword puts the secret on the concealed clipboard, then clears it after
// the timeout or on interrupt — and never clobbers a value the user copied since
// (spec §9).
func copyPassword(e vault.Entry, timeout time.Duration) error {
	pw := []byte(e.Password.Reveal())
	if err := clip.Copy(pw); err != nil {
		return err
	}
	for i := range pw { // best-effort wipe of our local copy
		pw[i] = 0
	}
	fmt.Fprintf(os.Stderr, "copied %s · %s — clears in %s (or on ctrl-c)\n", e.Source, e.Path, timeout)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	select {
	case <-time.After(timeout):
	case <-ctx.Done():
	}
	return clip.ClearIfUnchanged()
}
