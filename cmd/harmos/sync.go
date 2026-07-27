package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/pottom/harmos/internal/config"
	"github.com/pottom/harmos/internal/keyring"
	"github.com/pottom/harmos/internal/secret"
	"github.com/pottom/harmos/internal/source/pleasant"
)

func newSyncCmd() *cobra.Command {
	var configPath string
	var savePassword bool
	cmd := &cobra.Command{
		Use:   "sync [source]",
		Short: "Pull each Pleasant source's OfflinePackage into its local kdbx cache",
		Long: "Pull the OfflinePackage from each Pleasant source and write it to that " +
			"source's encrypted kdbx cache. With no argument, syncs every Pleasant " +
			"source; kdbx sources are skipped. This is an explicit, audited action. " +
			"Passwords come from the keyring when saved (else a prompt); --save-password " +
			"stores the master and each server password after a successful sync.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSync(cmd.Context(), configPath, args, savePassword, cmd.OutOrStdout())
		},
	}
	cmd.Flags().BoolVar(&savePassword, "save-password", false,
		"store the master and server passwords in the OS keyring after syncing")
	cmd.Flags().StringVar(&configPath, "config", "",
		"config file (default: $XDG_CONFIG_HOME/harmos/config.toml)")
	return cmd
}

func runSync(ctx context.Context, configPath string, args []string, savePassword bool, out io.Writer) error {
	if configPath == "" {
		p, err := config.DefaultPath()
		if err != nil {
			return err
		}
		configPath = p
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	targets := cfg.Sources
	if len(args) == 1 {
		p := cfg.Source(args[0])
		if p == nil {
			return fmt.Errorf("no such source %q", args[0])
		}
		targets = []config.Source{*p}
	}

	var pleasantTargets []config.Source
	for _, p := range targets {
		switch {
		case p.Type == config.Pleasant:
			pleasantTargets = append(pleasantTargets, p)
		case len(args) == 1:
			// named a kdbx source explicitly: the file is the source, no sync
			emitf(out, "%s is a kdbx source; nothing to sync\n", p.Name)
		}
	}
	if len(pleasantTargets) == 0 {
		emitf(out, "no Pleasant sources to sync\n")
		return nil
	}

	master, err := resolveSyncMaster()
	if err != nil {
		return err
	}

	failures := 0
	for _, p := range pleasantTargets {
		if err := syncOne(ctx, p, master, savePassword, out); err != nil {
			emitf(out, "  %s: FAILED — %v\n", p.Name, err)
			failures++
		}
	}
	if failures == len(pleasantTargets) {
		return fmt.Errorf("all %d source(s) failed", failures)
	}
	return nil
}

// resolveSyncMaster gets the master for encrypting the cache: HARMOS_MASTER, then
// the keyring, then a prompt.
func resolveSyncMaster() (secret.Secret, error) {
	if v, ok := os.LookupEnv("HARMOS_MASTER"); ok {
		return secret.New(v), nil
	}
	if pw, ok, _ := keyring.FetchMaster(); ok {
		return pw, nil
	}
	if !onTTY() {
		return secret.Secret{}, fmt.Errorf("no master password: set HARMOS_MASTER, or save it with `harmos save-password <pleasant-source>`")
	}
	return promptPassword("harmos master password: ")
}

// resolveServerPass gets a Pleasant source's server login password from the
// keyring, else a prompt. The bool reports whether it came from the keyring.
func resolveServerPass(p config.Source) (secret.Secret, bool, error) {
	if pw, ok, _ := keyring.FetchServer(p.Name); ok {
		return pw, true, nil
	}
	if !onTTY() {
		return secret.Secret{}, false, fmt.Errorf("no saved password for %q; run `harmos sync --save-password` on a terminal first", p.Name)
	}
	pw, err := promptPassword(fmt.Sprintf("  password for %s: ", p.User))
	return pw, false, err
}

func syncOne(ctx context.Context, p config.Source, master secret.Secret, savePassword bool, out io.Writer) error {
	emitf(out, "%s (%s):\n", p.Name, p.URL)

	serverPass, fromKeyring, err := resolveServerPass(p)
	if err != nil {
		return err
	}

	hc, err := pleasant.NewHTTPClient(p.CABundle)
	if err != nil {
		return err
	}
	c := pleasant.New(p.URL, pleasant.WithHTTPClient(hc))

	if err := c.Login(ctx, p.User, serverPass); err != nil {
		// A stale saved password shouldn't wedge sync — re-prompt once on a terminal.
		if !fromKeyring || !onTTY() {
			return err
		}
		emitf(out, "  saved password rejected — try again\n")
		if serverPass, err = promptPassword(fmt.Sprintf("  password for %s: ", p.User)); err != nil {
			return err
		}
		if err := c.Login(ctx, p.User, serverPass); err != nil {
			return err
		}
	}

	rep := &syncReporter{w: out}
	res, err := pleasant.Sync(ctx, c, p.URL, pleasant.SyncOptions{
		Comment:   "harmos sync",
		CachePath: p.Cache,
		Master:    master,
		Report:    &pleasant.Reporter{Phase: rep.phase, Bytes: rep.bytes},
	})
	rep.endLine() // finish any open progress line before the result/error
	if err != nil {
		return err
	}

	if savePassword {
		if err := keyring.StoreMaster(master); err != nil {
			emitf(out, "  warning: could not save the master to the keyring: %v\n", err)
		}
		if err := keyring.StoreServer(p.Name, serverPass); err != nil {
			emitf(out, "  warning: could not save the server password: %v\n", err)
		} else {
			emitf(out, "  saved passwords to the keyring\n")
		}
	}

	emitf(out, "  synced %d entries, %d folders, %d attachments → %s (expiry %s)\n",
		res.Entries, res.Folders, res.Attachments, p.Cache, res.Expiry)
	return nil
}

// syncReporter prints live sync progress: a line per phase, with the download
// phase updating in place as bytes arrive.
type syncReporter struct {
	w    io.Writer
	open bool // the current line has no trailing newline yet
}

func (r *syncReporter) phase(name string) {
	r.endLine()
	emitf(r.w, "  %s…", name)
	r.open = true
}

func (r *syncReporter) bytes(done, total int64) {
	if total > 0 {
		pct := min(done*100/total, 100)
		emitf(r.w, "\r  downloading offline package… %s / %s (%d%%)  ", humanBytes(done), humanBytes(total), pct)
	} else {
		emitf(r.w, "\r  downloading offline package… %s  ", humanBytes(done))
	}
	r.open = true
}

func (r *syncReporter) endLine() {
	if r.open {
		emitf(r.w, "\n")
		r.open = false
	}
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGT"[exp])
}

// emitf writes progress to the command's output; the error is not actionable.
func emitf(w io.Writer, format string, a ...any) {
	_, _ = fmt.Fprintf(w, format, a...)
}

// promptPassword reads a password from the terminal without echo. The prompt
// goes to stderr so stdout stays clean for scripting.
func promptPassword(label string) (secret.Secret, error) {
	fmt.Fprint(os.Stderr, label)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return secret.Secret{}, err
	}
	return secret.FromBytes(b), nil
}
