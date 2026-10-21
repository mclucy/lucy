package cli

import (
	"fmt"
	"os"

	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/internal/cli/help"
	"github.com/mclucy/lucy/operation"
	"github.com/mclucy/lucy/report"
	"github.com/spf13/cobra"
)

const (
	flagInstallLockedName       = "locked"
	flagInstallOfflineName      = "offline"
	flagInstallWithOptionalName = "with-optional"
	flagInstallForceName        = "force"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: help.InstallShort,
	Args:  cobra.NoArgs,
	RunE:  actionInstall,
}

func NewInstallCommand() *cobra.Command {
	installCmd.Flags().Bool(
		flagInstallLockedName,
		false,
		"Do not resolve new versions; fail when the lock cannot satisfy",
	)
	installCmd.Flags().Bool(
		flagInstallOfflineName,
		false,
		"Disallow network acquisition; fail if any file is missing or mismatched",
	)
	installCmd.Flags().Bool(
		flagInstallWithOptionalName,
		false,
		"Also install optional dependencies and record them as explicit packages",
	)
	installCmd.Flags().BoolP(
		flagInstallForceName,
		"f",
		false,
		"Complete the installation even when packages cannot satisfy their declared modules",
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
	force, _ := cmd.Flags().GetBool(flagInstallForceName)

	opts := install.Options{
		Locked:          locked,
		Offline:         offline,
		Force:           force,
		WithOptional:    withOptional,
		PublishManifest: false,
	}

	diagnostics, warnings, err := operation.Sync(cmd.Context(), workDir, opts)
	for _, diagnostic := range diagnostics {
		report.Warn(fmt.Errorf("%s: %s", diagnostic.Path, diagnostic.Message))
	}
	for _, warning := range warnings {
		report.Warn(warning)
	}
	return err
}
