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
		Long: "Prompt for a source's password and store it in the OS keyring so harmos " +
			"can unlock it without asking. For a Pleasant source this saves the shared " +
			"harmos master password (which unlocks every Pleasant cache); for a local " +
			"kdbx source it saves that file's own password. The password never touches " +
			"the config file.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfigAt(configPath)
			if err != nil {
				return err
			}
			p := cfg.Profile(args[0])
			if p == nil {
				return fmt.Errorf("no profile named %q", args[0])
			}
			if p.Type == config.Pleasant {
				return saveMaster(cmd.OutOrStdout())
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

func newForgetCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "forget <name>",
		Short: "Remove a source's password from the OS keyring",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			// If we can see the config and the source is Pleasant, forget the shared
			// master; otherwise forget the per-source entry.
			if cfg, err := loadConfigAt(configPath); err == nil {
				if p := cfg.Profile(name); p != nil && p.Type == config.Pleasant {
					if err := keyring.ForgetMaster(); err != nil {
						return err
					}
					emitf(cmd.OutOrStdout(), "forgot the harmos master password from the keyring (if any)\n")
					return nil
				}
			}
			if err := keyring.Forget(name); err != nil {
				return err
			}
			emitf(cmd.OutOrStdout(), "forgot the keyring password for %q (if any)\n", name)
			return nil
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "config file (default: $XDG_CONFIG_HOME/harmos/config.toml)")
	return cmd
}
