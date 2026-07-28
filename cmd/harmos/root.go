package main

import (
	"github.com/spf13/cobra"

	"github.com/pottom/harmos/internal/version"
)

func newRootCmd() *cobra.Command {
	var configPath string
	root := &cobra.Command{
		Use:          "harmos",
		Short:        "Terminal password client for Pleasant Password Server and local .kdbx files",
		Version:      version.Version,
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUI(configPath)
		},
	}
	root.Flags().StringVar(&configPath, "config", "", "config file (default: $XDG_CONFIG_HOME/harmos/config.toml)")
	root.AddCommand(newSyncCmd())
	root.AddCommand(newLsCmd())
	root.AddCommand(newGetCmd())
	root.AddCommand(newGenCmd())
	root.AddCommand(newUpdateCmd())
	root.AddCommand(newAddSourceCmd())
	root.AddCommand(newRemoveSourceCmd())
	root.AddCommand(newSourcesCmd())
	root.AddCommand(newSavePasswordCmd())
	root.AddCommand(newRemovePasswordCmd())
	root.AddCommand(newThemesCmd())
	return root
}
