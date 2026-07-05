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
	flagWithOptionalName = "with-optional"
	flagLockedName       = "locked"
	flagOfflineName      = "offline"
)

var addCmd = &cobra.Command{
	Use:   "add",
	Short: help.AddShort,
	Args:  cobra.MinimumNArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		ecosystem, _ := cmd.Flags().GetString(FlagEcosystem)
		if ecosystem != "" && !types.Ecosystem(ecosystem).Valid() {
			return fmt.Errorf("invalid ecosystem %q", ecosystem)
		}
		return nil
	},
	RunE: actionAdd,
}

func NewAddCommand() *cobra.Command {
	addCmd.Flags().Bool(
		flagWithOptionalName,
		false,
		"Also install optional upstream dependencies",
	)
	addCmd.Flags().Bool(
		flagLockedName,
		false,
		"Do not resolve new versions; fail when the lock cannot satisfy",
	)
	addCmd.Flags().Bool(
		flagOfflineName,
		false,
		"Disallow network access; fail if a needed file is not already available locally",
	)
	AddEcosystemFlag(addCmd)
	return addCmd
}

func actionAdd(cmd *cobra.Command, args []string) error {
	target, err := ResolveCommandTarget(cmd)
	if err != nil {
		return err
	}
	withOptional, _ := cmd.Flags().GetBool(flagWithOptionalName)
	if target.Registered {
		return DispatchPackageTask(
			cmd,
			target,
			server.PackageTaskRequest{
				Name: server.TaskAdd,
				Args: append([]string(nil), args...),
				AddOptions: server.PackageTaskAddOpts{
					WithOptional: withOptional,
				},
			},
		)
	}
	return RunInTargetWorkDir(target, func() error {
		return RunAddTask(cmd, args, target)
	})
}

// RunAddTask executes the add action for a daemon-dispatched package task.
func RunAddTask(cmd *cobra.Command, args []string, target CommandTarget) error {
	withOptional, _ := cmd.Flags().GetBool(flagWithOptionalName)
	locked, _ := cmd.Flags().GetBool(flagLockedName)
	offline, _ := cmd.Flags().GetBool(flagOfflineName)

	ecosystem, _ := cmd.Flags().GetString(FlagEcosystem)

	opts := install.Options{
		Locked:          locked,
		Offline:         offline,
		WithOptional:    withOptional,
		PublishManifest: false,
	}

	diagnostics, err := operation.Add(cmd.Context(), target.WorkDir, args, types.Ecosystem(ecosystem), opts)
	for _, diagnostic := range diagnostics {
		report.Warn(fmt.Errorf("%s: %s", diagnostic.Path, diagnostic.Message))
	}
	if err != nil {
		return err
	}
	MarkPendingRestartIfRunning(target, "package intent changed")
	return nil
}
