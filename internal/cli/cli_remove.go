package cli

import (
	"fmt"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/internal/cli/help"
	"github.com/spf13/cobra"
)

const (
	flagRemoveLockedName  = "locked"
	flagRemoveOfflineName = "offline"
)

func NewRemoveCommand(
	contracts *JSONContracts,
	inv *Invocation,
	deps *Dependencies,
) *cobra.Command {
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
		RunE: func(cmd *cobra.Command, args []string) error {
			return actionRemove(cmd, inv, deps, args)
		},
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

func actionRemove(
	cmd *cobra.Command,
	inv *Invocation,
	deps *Dependencies,
	args []string,
) error {
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

	output := newInstallOutput(cmd, inv)
	defer output.Close()
	opts.Trace = output.Trace()
	ctx, stop := CommandContext(cmd)
	defer stop()
	result, err := deps.Operations.Remove(ctx, inv.Directory, args, ecosystem, opts)
	if err != nil {
		return output.Failure(err)
	}
	return output.Render(result)
}
