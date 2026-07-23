package main

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/pottom/harmos/internal/keyring"
)

func newSavePasswordCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "save-password <name>",
		Short: "Store a source's password in the OS keyring",
		Long: "Prompt for a source's password and store it in the OS keyring so " +
			"harmos can unlock it without asking. The password never touches the " +
			"config file.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfigAt(configPath)
			if err != nil {
				return err
			}
			if cfg.Profile(args[0]) == nil {
				return fmt.Errorf("no profile named %q", args[0])
			}
			return savePasswordFor(args[0], cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "config file (default: $XDG_CONFIG_HOME/harmos/config.toml)")
	return cmd
}

// savePasswordFor prompts for a password on the terminal and stores it in the
// keyring under the given profile name.
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

func newForgetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "forget <name>",
		Short: "Remove a source's password from the OS keyring",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := keyring.Forget(args[0]); err != nil {
				return err
			}
			emitf(cmd.OutOrStdout(), "forgot the keyring password for %q (if any)\n", args[0])
			return nil
		},
	}
	return cmd
}
