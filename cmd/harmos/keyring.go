package main

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/pottom/harmos/internal/config"
	"github.com/pottom/harmos/internal/keyring"
)

func newSavePasswordCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "save-password <name>",
		Short: "Store a source's password in the OS keyring",
		Long: "Prompt for a source's password(s) and store them in the OS keyring so " +
			"harmos never asks again. A Pleasant source saves the shared harmos master " +
			"(if not already saved) and that source's server login password; a local " +
			"kdbx source saves that file's own password. Nothing touches the config file.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfigAt(configPath)
			if err != nil {
				return err
			}
			p := cfg.Source(args[0])
			if p == nil {
				return fmt.Errorf("no source named %q", args[0])
			}
			if p.Type == config.Pleasant {
				return savePleasantPasswords(args[0], p.User, cmd.OutOrStdout())
			}
			return savePasswordFor(args[0], cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "config file (default: $XDG_CONFIG_HOME/harmos/config.toml)")
	return cmd
}

// savePasswordFor prompts for a password and stores it in the keyring under a
// local kdbx source's name.
func savePasswordFor(name string, out io.Writer) error {
	if !onTTY() {
		return fmt.Errorf("a terminal is required to read the password to save")
	}
	pw, err := promptPassword(fmt.Sprintf("password to save for %s: ", name))
	if err != nil {
		return err
	}
	if err := keyring.Store(name, pw); err != nil {
		return fmt.Errorf("store in keyring: %w", err)
	}
	emitf(out, "saved the keyring password for %q\n", name)
	return nil
}

// saveMaster prompts for the harmos master password and stores it in the keyring;
// it unlocks every Pleasant cache.
func saveMaster(out io.Writer) error {
	if !onTTY() {
		return fmt.Errorf("a terminal is required to read the master password to save")
	}
	pw, err := promptPassword("harmos master password to save: ")
	if err != nil {
		return err
	}
	if err := keyring.StoreMaster(pw); err != nil {
		return fmt.Errorf("store in keyring: %w", err)
	}
	emitf(out, "saved the harmos master password in the keyring (unlocks all Pleasant sources)\n")
	return nil
}

// savePleasantPasswords stores a Pleasant source's secrets: the shared master
// (only when not already saved) and this source's server login password.
func savePleasantPasswords(name, user string, out io.Writer) error {
	if !onTTY() {
		return fmt.Errorf("a terminal is required to read the passwords to save")
	}
	if _, ok, _ := keyring.FetchMaster(); !ok {
		master, err := promptPassword("harmos master password to save: ")
		if err != nil {
			return err
		}
		if err := keyring.StoreMaster(master); err != nil {
			return fmt.Errorf("store master: %w", err)
		}
		emitf(out, "saved the harmos master password to the keyring\n")
	}
	srv, err := promptPassword(fmt.Sprintf("server password for %s to save: ", user))
	if err != nil {
		return err
	}
	if err := keyring.StoreServer(name, srv); err != nil {
		return fmt.Errorf("store server password: %w", err)
	}
	emitf(out, "saved %q's server password to the keyring\n", name)
	return nil
}
