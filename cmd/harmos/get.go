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
	"github.com/pottom/harmos/internal/otp"
	"github.com/pottom/harmos/internal/search"
	"github.com/pottom/harmos/internal/vault"
)

func newGetCmd() *cobra.Command {
	var configPath string
	var doCopy, showOTP bool
	cmd := &cobra.Command{
		Use:   "get <query>",
		Short: "Print (or --copy) the matching password; refuses to guess when ambiguous",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGet(configPath, args[0], doCopy, showOTP, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "config file (default: $XDG_CONFIG_HOME/harmos/config.toml)")
	cmd.Flags().BoolVar(&doCopy, "copy", false, "copy to the clipboard (concealed), auto-cleared after the timeout")
	cmd.Flags().BoolVar(&showOTP, "otp", false, "print (or --copy) the current TOTP code instead of the password")
	return cmd
}

func runGet(configPath, query string, doCopy, showOTP bool, out io.Writer) error {
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
		if showOTP {
			return emitOTP(best, query, doCopy, cfg.ClipboardTimeout.Duration, out)
		}
		if doCopy {
			return copyConcealed([]byte(best.Password.Reveal()), best.Source+" · "+best.Path, cfg.ClipboardTimeout.Duration)
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

// emitOTP prints (or copies) the entry's current TOTP code.
func emitOTP(e vault.Entry, query string, doCopy bool, timeout time.Duration, out io.Writer) error {
	if e.TOTP == "" {
		return fmt.Errorf("%q has no TOTP", query)
	}
	k, err := otp.Parse(e.TOTP)
	if err != nil {
		return fmt.Errorf("bad TOTP for %q: %w", query, err)
	}
	now := time.Now()
	code := k.Code(now)
	if doCopy {
		return copyConcealed([]byte(code), e.Source+" · "+e.Path+" (TOTP)", timeout)
	}
	// code → stdout, provenance → stderr, so otp=$(harmos get --otp x) is clean.
	emitf(out, "%s\n", code)
	fmt.Fprintf(os.Stderr, "%s · %s · TOTP valid %ds\n", e.Source, e.Path, k.Remaining(now))
	return nil
}

// copyConcealed puts a value on the concealed clipboard, then clears it after the
// timeout or on interrupt — and never clobbers a value the user copied since
// (spec §9).
func copyConcealed(value []byte, label string, timeout time.Duration) error {
	if err := clip.Copy(value); err != nil {
		return err
	}
	for i := range value { // best-effort wipe of our local copy
		value[i] = 0
	}
	fmt.Fprintf(os.Stderr, "copied %s — clears in %s (or on ctrl-c)\n", label, timeout)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	select {
	case <-time.After(timeout):
	case <-ctx.Done():
	}
	return clip.ClearIfUnchanged()
}
