package cli

import (
	"fmt"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/internal/cli/help"
	"github.com/mclucy/lucy/operation"
	"github.com/mclucy/lucy/server"
	"github.com/spf13/cobra"
)

const (
	flagRemoveLockedName  = "locked"
	flagRemoveOfflineName = "offline"
)

func NewRemoveCommand(contracts *JSONContracts) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "remove",
		Short:   help.RemoveShort,
		Args:    cobra.MinimumNArgs(1),
		Aliases: []string{"rm"},
		PreRunE: func(cmd *cobra.Command, args []string) error {
			ecosystem, _ := cmd.Flags().GetString(FlagEcosystem)
			if ecosystem != "" && !catalog.Ecosystem(ecosystem).Valid() {
				return fmt.Errorf("invalid ecosystem %q", ecosystem)
			}
			return nil
		},
		RunE: actionRemove,
	}
	installJSON.Bind(contracts, cmd)
	cmd.Flags().Bool(
		flagRemoveLockedName,
		false,
		"Do not resolve new versions; fail when the lock cannot satisfy",
	)
	cmd.Flags().Bool(
		flagRemoveOfflineName,
		false,
		"Disallow network acquisition for any re-resolution this triggers",
	)
	AddEcosystemFlag(cmd)
	return cmd
}

func actionRemove(cmd *cobra.Command, args []string) error {
	target, err := ResolveCommandTarget(cmd)
	if err != nil {
		return fmt.Errorf("could not determine working directory: %w", err)
	}
	if target.Registered {
		return DispatchPackageTask(cmd, target, server.PackageTaskRequest{Name: server.TaskRemove, Args: args})
	}
	return RunInTargetWorkDir(target, func() error { return RunRemoveTask(cmd, args, target) })
}

// RunRemoveTask applies removal with the selected ecosystem and acquisition policy.
func RunRemoveTask(cmd *cobra.Command, args []string, target CommandTarget) error {
	workDir := target.WorkDir

	locked, _ := cmd.Flags().GetBool(flagRemoveLockedName)
	offline, _ := cmd.Flags().GetBool(flagRemoveOfflineName)

	ecosystemArg, _ := cmd.Flags().GetString(FlagEcosystem)
	ecosystem := catalog.Ecosystem(ecosystemArg)

	opts := install.Options{
		Locked:          locked,
		Offline:         offline,
		WithOptional:    false,
		PublishManifest: false,
	}

	output := newInstallOutput(cmd)
	defer output.Close()
	opts.Trace = output.Trace()
	ctx, stop := CommandContext(cmd)
	defer stop()
	result, err := operation.Remove(ctx, workDir, args, ecosystem, opts)
	if err != nil {
		return output.Failure(err)
	}
	return output.Render(result)
}
