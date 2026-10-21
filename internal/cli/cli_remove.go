package cli

import (
	"fmt"

	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/internal/cli/help"
	"github.com/mclucy/lucy/operation"
	"github.com/mclucy/lucy/report"
	"github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/types"
	"github.com/spf13/cobra"
)

const (
	flagRemoveLockedName  = "locked"
	flagRemoveOfflineName = "offline"
)

var removeCmd = &cobra.Command{
	Use:   "remove",
	Short: help.RemoveShort,
	Args:  cobra.MinimumNArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		ecosystem, _ := cmd.Flags().GetString(FlagEcosystem)
		if ecosystem != "" && !types.Ecosystem(ecosystem).Valid() {
			return fmt.Errorf("invalid ecosystem %q", ecosystem)
		}
		return nil
	},
	RunE: actionRemove,
}

func NewRemoveCommand() *cobra.Command {
	removeCmd.Flags().Bool(
		flagRemoveLockedName,
		false,
		"Do not resolve new versions; fail when the lock cannot satisfy",
	)
	removeCmd.Flags().Bool(
		flagRemoveOfflineName,
		false,
		"Disallow network acquisition for any re-resolution this triggers",
	)
	AddEcosystemFlag(removeCmd)
	return removeCmd
}

func actionRemove(cmd *cobra.Command, args []string) error {
	target, err := ResolveCommandTarget(cmd)
	if err != nil {
		return err
	}
	if target.Registered {
		return DispatchPackageTask(
			cmd,
			target,
			server.PackageTaskRequest{
				Name: server.TaskRemove,
				Args: append([]string(nil), args...),
			},
		)
	}
	return RunInTargetWorkDir(target, func() error {
		return RunRemoveTask(cmd, args, target)
	})
}

// RunRemoveTask executes the remove action for a daemon-dispatched package task.
func RunRemoveTask(cmd *cobra.Command, args []string, target CommandTarget) error {
	locked, _ := cmd.Flags().GetBool(flagRemoveLockedName)
	offline, _ := cmd.Flags().GetBool(flagRemoveOfflineName)

	ecosystemArg, _ := cmd.Flags().GetString(FlagEcosystem)
	ecosystem := types.Ecosystem(ecosystemArg)

	opts := install.Options{
		Locked:          locked,
		Offline:         offline,
		WithOptional:    false,
		PublishManifest: false,
	}

	diagnostics, err := operation.Remove(cmd.Context(), target.WorkDir, args, ecosystem, opts)
	for _, diagnostic := range diagnostics {
		report.Warn(fmt.Errorf("%s: %s", diagnostic.Path, diagnostic.Message))
	}
	if err != nil {
		return err
	}
	MarkPendingRestartIfRunning(target, "package intent changed")
	return nil
}
