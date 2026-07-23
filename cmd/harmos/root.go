package main

import "github.com/spf13/cobra"

// version is overridden at release time by the build.
var version = "0.0.0-dev"

func newRootCmd() *cobra.Command {
	var configPath string
	root := &cobra.Command{
		Use:          "harmos",
		Short:        "Read-only terminal password client for Pleasant Password Server and local .kdbx files",
		Version:      version,
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
	root.AddCommand(newAddKdbxCmd())
	root.AddCommand(newSourcesCmd())
	root.AddCommand(newSavePasswordCmd())
	root.AddCommand(newForgetCmd())
	return root
}
