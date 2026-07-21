package main

import "github.com/spf13/cobra"

// version is overridden at release time by the build.
var version = "0.0.0-dev"

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:          "harmos",
		Short:        "Read-only terminal password client for Pleasant Password Server and local .kdbx files",
		Version:      version,
		SilenceUsage: true,
	}
	root.AddCommand(newSyncCmd())
	return root
}
