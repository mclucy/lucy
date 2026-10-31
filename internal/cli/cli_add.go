package cli

import (
	"fmt"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/internal/cli/help"
	"github.com/spf13/cobra"
)

const (
	flagWithOptionalName = "with-optional"
	flagLockedName       = "locked"
	flagOfflineName      = "offline"
)

func NewAddCommand(
	contracts *JSONContracts,
	inv *Invocation,
	deps *Dependencies,
) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "add",
		Short:   help.AddShort,
		Args:    cobra.MinimumNArgs(1),
		Aliases: []string{"a"},
		PreRunE: func(cmd *cobra.Command, args []string) error {
			ecosystem, _ := cmd.Flags().GetString(FlagEcosystem)
			if ecosystem != "" && !catalog.Ecosystem(ecosystem).Valid() {
				return fmt.Errorf("invalid ecosystem %q", ecosystem)
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return actionAdd(cmd, inv, deps, args)
		},
	}
	installJSON.Bind(contracts, cmd)
	cmd.Flags().Bool(
		flagWithOptionalName,
		false,
		"Also install optional upstream dependencies",
	)
	cmd.Flags().Bool(
		flagLockedName,
		false,
		"Do not resolve new versions; fail when the lock cannot satisfy",
	)
	cmd.Flags().Bool(
		flagOfflineName,
		false,
		"Disallow network access; fail if a needed file is not already available locally",
	)
	AddEcosystemFlag(cmd)
	return cmd
}

func actionAdd(cmd *cobra.Command, inv *Invocation, deps *Dependencies, args []string) error {
	target, err := ResolveCommandTarget(cmd, inv)
	if err != nil {
		return fmt.Errorf("could not determine working directory: %w", err)
	}
	if target.Registered {
		return DispatchPackageTask(cmd, inv, target, PackageTaskRequest{Name: TaskAdd, Args: args})
	}
	return RunAddTask(cmd, inv, deps, args)
}

// RunAddTask runs the normal command output and operation in a selected workspace.
func RunAddTask(
	cmd *cobra.Command,
	inv *Invocation,
	deps *Dependencies,
	args []string,
) error {
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

	output := newInstallOutput(cmd, inv)
	defer output.Close()
	opts.Trace = output.Trace()
	ctx, stop := CommandContext(cmd)
	defer stop()
	result, err := deps.Operations.Add(
		ctx,
		inv.Directory,
		args,
		catalog.Ecosystem(ecosystem),
		opts,
	)
	if err != nil {
		return output.Failure(err)
	}
	return output.Render(result)
}
