package cli

import (
	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/internal/cli/help"
	"github.com/mclucy/lucy/operation"
	"github.com/mclucy/lucy/server"
	"github.com/spf13/cobra"
)

const (
	flagInstallLockedName       = "locked"
	flagInstallOfflineName      = "offline"
	flagInstallWithOptionalName = "with-optional"
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
	return installCmd
}

func actionInstall(cmd *cobra.Command, _ []string) error {
	target, err := ResolveCommandTarget(cmd)
	if err != nil {
		return err
	}
	if target.Registered {
		return DispatchPackageTask(
			cmd,
			target,
			server.PackageTaskRequest{Name: server.TaskInstall},
		)
	}
	return RunInTargetWorkDir(target, func() error {
		return RunInstallTask(cmd, target)
	})
}

// RunInstallTask executes the install action for a daemon-dispatched package task.
func RunInstallTask(cmd *cobra.Command, target CommandTarget) error {
	locked, _ := cmd.Flags().GetBool(flagInstallLockedName)
	offline, _ := cmd.Flags().GetBool(flagInstallOfflineName)
	withOptional, _ := cmd.Flags().GetBool(flagInstallWithOptionalName)

	opts := install.Options{
		Locked:          locked,
		Offline:         offline,
		WithOptional:    withOptional,
		PublishManifest: false,
	}

	if err := operation.Sync(cmd.Context(), target.WorkDir, opts); err != nil {
		return err
	}
	MarkPendingRestartIfRunning(target, "install changed runtime files")
	return nil
}
