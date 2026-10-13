package cli

import (
	"fmt"
	"os"

	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/internal/cli/message"
	"github.com/spf13/cobra"
)

const (
	flagInstallLockedName       = "locked"
	flagInstallOfflineName      = "offline"
	flagInstallWithOptionalName = "with-optional"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: message.InstallShort,
	Args:  cobra.NoArgs,
	RunE:  WithErrorLogging(actionInstall),
}

func NewInstallCommand() *cobra.Command {
	installCmd.Flags().Bool(
		flagInstallLockedName,
		false,
		"Fail instead of re-resolving when the lock is missing or stale",
	)
	installCmd.Flags().Bool(
		flagInstallOfflineName,
		false,
		"Disallow network acquisition; fail if any file is missing or mismatched",
	)
	installCmd.Flags().Bool(
		flagInstallWithOptionalName,
		false,
		"Re-resolve to install optional upstream dependencies and record them as explicit requirements",
	)
	return installCmd
}

func actionInstall(cmd *cobra.Command, _ []string) error {
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("could not determine working directory: %w", err)
	}

	locked, _ := cmd.Flags().GetBool(flagInstallLockedName)
	offline, _ := cmd.Flags().GetBool(flagInstallOfflineName)
	withOptional, _ := cmd.Flags().GetBool(flagInstallWithOptionalName)

	opts := install.Options{
		Locked:          locked,
		Offline:         offline,
		WithOptional:    withOptional,
		PublishManifest: false,
	}

	return install.Run(cmd.Context(), workDir, opts)
}
