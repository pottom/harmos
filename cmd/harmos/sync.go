package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/pottom/harmos/internal/config"
	"github.com/pottom/harmos/internal/secret"
	"github.com/pottom/harmos/internal/source/pleasant"
)

func newSyncCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "sync [profile]",
		Short: "Pull each Pleasant source's OfflinePackage into its local kdbx cache",
		Long: "Pull the OfflinePackage from each Pleasant source and write it to that " +
			"source's encrypted kdbx cache. With no argument, syncs every Pleasant " +
			"source; kdbx sources are skipped. This is an explicit, audited action.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSync(cmd.Context(), configPath, args, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "",
		"config file (default: $XDG_CONFIG_HOME/harmos/config.toml)")
	return cmd
}

func runSync(ctx context.Context, configPath string, args []string, out io.Writer) error {
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

	targets := cfg.Profiles
	if len(args) == 1 {
		p := cfg.Profile(args[0])
		if p == nil {
			return fmt.Errorf("no such profile %q", args[0])
		}
		targets = []config.Profile{*p}
	}

	var pleasantTargets []config.Profile
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

	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return fmt.Errorf("sync needs a terminal to prompt for the master and server passwords")
	}
	master, err := promptPassword("harmos master password: ")
	if err != nil {
		return err
	}

	failures := 0
	for _, p := range pleasantTargets {
		if err := syncOne(ctx, p, master, out); err != nil {
			emitf(out, "  %s: FAILED — %v\n", p.Name, err)
			failures++
		}
	}
	if failures == len(pleasantTargets) {
		return fmt.Errorf("all %d source(s) failed", failures)
	}
	return nil
}

func syncOne(ctx context.Context, p config.Profile, master secret.Secret, out io.Writer) error {
	emitf(out, "%s (%s):\n", p.Name, p.URL)
	serverPass, err := promptPassword(fmt.Sprintf("  password for %s: ", p.User))
	if err != nil {
		return err
	}

	hc, err := pleasant.NewHTTPClient(p.CABundle)
	if err != nil {
		return err
	}
	c := pleasant.New(p.URL, pleasant.WithHTTPClient(hc))
	if err := c.Login(ctx, p.User, serverPass); err != nil {
		return err
	}

	res, err := pleasant.Sync(ctx, c, p.URL, pleasant.SyncOptions{
		Comment:   "harmos sync",
		CachePath: p.Cache,
		Master:    master,
	})
	if err != nil {
		return err
	}
	emitf(out, "  synced %d entries, %d folders, %d attachments → %s (expiry %s)\n",
		res.Entries, res.Folders, res.Attachments, p.Cache, res.Expiry)
	return nil
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
