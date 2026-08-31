package cmd

import (
	"context"
	"fmt"

	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/internal/cli"
	"github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/state"
	"github.com/mclucy/lucy/types"
	"github.com/spf13/cobra"
)

var removeCmd = &cobra.Command{
	Use:   "remove",
	Short: "Remove packages under explicit operator control",
	Args:  cobra.MinimumNArgs(1),
	ValidArgsFunction: func(
		cmd *cobra.Command,
		args []string,
		toComplete string,
	) ([]string, cobra.ShellCompDirective) {
		return cli.CompletePackageIDSuggestions(
			context.Background(),
			"remove",
			toComplete,
		)
	},
	RunE: cli.WithErrorLogging(actionRemove),
}

func init() {
	cli.AddNoStyleFlag(removeCmd)
	cli.AddPlatformFlag(removeCmd)
	rootCmd.AddCommand(removeCmd)
}

func actionRemove(cmd *cobra.Command, args []string) error {
	target, err := cli.ResolveCommandTarget(cmd)
	if err != nil {
		return err
	}
	if target.Registered {
		return cli.DispatchPackageTask(
			cmd,
			target,
			server.PackageTaskRequest{
				Name: server.TaskRemove,
				Args: append([]string(nil), args...),
			},
		)
	}
	return cli.RunInTargetWorkDir(target, func() error {
		return actionRemoveAt(cmd, args, target)
	})
}

func actionRemoveAt(cmd *cobra.Command, args []string, target cli.CommandTarget) error {
	workDir := target.WorkDir
	hasLucyState, err := cli.LucyStateDirExists(workDir)
	if err != nil {
		return err
	}
	if !hasLucyState {
		return fmt.Errorf("lucy state is not initialized")
	}

	stateSvc := state.NewProjectStateService(workDir)
	if err := stateSvc.Load(cmd.Context()); err != nil {
		return fmt.Errorf("load lucy state: %w", err)
	}
	if stateSvc.Manifest() == nil {
		return fmt.Errorf("manifest is required for remove")
	}

	platformArg, _ := cmd.Flags().GetString(cli.FlagPlatform)
	platform := types.Ecosystem(platformArg)
	if platform != types.EcoUnspecified && !platform.Valid() {
		return fmt.Errorf("--platform must name a supported ecosystem")
	}
	ids := make([]types.VersionedPackageRef, 0, len(args))
	for _, arg := range args {
		request, err := input.Parse(arg)
		if err != nil {
			return err
		}
		if platform != types.EcoUnspecified {
			request.Eco = platform
		}
		ids = append(ids, types.VersionedPackageRef{PackageRef: request.PackageRef, Eco: request.Eco, Version: request.Version})
	}

	manifest := state.UpdateManifestRolesForRemove(
		stateSvc.Manifest(),
		ids,
		stateSvc.Lock(),
	)
	if stateSvc.Lock() == nil {
		return stateSvc.Save(cmd.Context(), manifest, nil)
	}

	lock := state.PruneLockForManifest(stateSvc.Lock(), manifest)
	if err := stateSvc.Save(cmd.Context(), manifest, lock); err != nil {
		return fmt.Errorf("update state: %w", err)
	}

	cli.MarkPendingRestartIfRunning(target, "package intent changed")
	return nil
}
