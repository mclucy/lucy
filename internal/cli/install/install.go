package install

import (
	"fmt"
	"os"

	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/internal/cli"
	"github.com/spf13/cobra"
)

const (
	flagLockedName       = "locked"
	flagOfflineName      = "offline"
	flagWithOptionalName = "with-optional"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Converge Lucy-managed runtime state from the lockfile",
	Args:  cobra.NoArgs,
	RunE:  cli.WithErrorLogging(actionInstall),
}

func NewCommand() *cobra.Command {
	installCmd.Flags().Bool(
		flagLockedName,
		false,
		"Fail instead of re-resolving when the lock is missing or stale",
	)
	installCmd.Flags().Bool(
		flagOfflineName,
		false,
		"Disallow network acquisition; fail if any file is missing or mismatched",
	)
	installCmd.Flags().Bool(
		flagWithOptionalName,
		false,
		"Also install optional requirements marked enabled: false",
	)
	cli.AddNoStyleFlag(installCmd)
	return installCmd
}

func actionInstall(cmd *cobra.Command, _ []string) error {
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("could not determine working directory: %w", err)
	}

	locked, _ := cmd.Flags().GetBool(flagLockedName)
	offline, _ := cmd.Flags().GetBool(flagOfflineName)
	withOptional, _ := cmd.Flags().GetBool(flagWithOptionalName)

	opts := install.Options{
		Locked:          locked,
		Offline:         offline,
		WithOptional:    withOptional,
		PublishManifest: false,
	}

	return install.Run(cmd.Context(), workDir, opts)
}
