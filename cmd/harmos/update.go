package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/pottom/harmos/internal/selfupdate"
	"github.com/pottom/harmos/internal/updater"
	"github.com/pottom/harmos/internal/version"
)

// newUpdateCmd replaces the running binary with the latest GitHub release — the
// actionable other half of the header's update marker. It only reads public
// release assets and sends nothing about the machine.
func newUpdateCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "update",
		Aliases:      []string{"self-update"},
		Short:        "Update harmos to the latest release",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			emitf(cmd.ErrOrStderr(), "Checking for a newer release…\n")
			tag, err := selfupdate.Update(version.Version)
			switch {
			case errors.Is(err, selfupdate.ErrUpToDate):
				emitf(cmd.OutOrStdout(), "harmos %s is already the latest release.\n", version.Version)
				return nil
			case errors.Is(err, updater.ErrNoReleases):
				return fmt.Errorf("no releases published yet — see https://github.com/pottom/harmos/releases")
			case errors.Is(err, selfupdate.ErrUnsupported):
				return fmt.Errorf("%w — reinstall from https://github.com/pottom/harmos/releases", err)
			case err != nil:
				return err
			}
			emitf(cmd.OutOrStdout(), "Updated harmos to %s.\n", tag)
			return nil
		},
	}
}
