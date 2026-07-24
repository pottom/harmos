package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/spf13/cobra"

	"github.com/pottom/harmos/internal/config"
	"github.com/pottom/harmos/internal/theme"
)

// applyConfiguredTheme selects the active TUI theme from the config: a built-in
// name, or a custom themes/<name>.toml next to the config file. Unknown names
// warn and keep the charm default.
func applyConfiguredTheme(cfg *config.Config, configPath string) {
	name := cfg.Theme
	if name == "" || name == "charm" {
		return // charm is the default
	}
	if t, ok := theme.Builtin(name); ok {
		theme.Apply(t)
		return
	}
	path := filepath.Join(filepath.Dir(configPath), "themes", name+".toml")
	var t theme.Theme
	if _, err := toml.DecodeFile(path, &t); err != nil {
		fmt.Fprintf(os.Stderr, "warning: theme %q not found (%v); using charm\n", name, err)
		return
	}
	if t.Name == "" {
		t.Name = name
	}
	theme.Apply(t)
}

func newThemesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "themes",
		Short: "List the built-in color themes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			for _, n := range theme.Names() {
				emitf(out, "%s\n", n)
			}
			emitf(out, "\nselect one with `theme = \"<name>\"` in the config,\n")
			emitf(out, "or add a custom theme at $XDG_CONFIG_HOME/harmos/themes/<name>.toml\n")
			return nil
		},
	}
}
