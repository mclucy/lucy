package cli

import (
	"fmt"
	"os"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/internal/cli/help"
	"github.com/mclucy/lucy/operation"
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
		if ecosystem != "" && !catalog.Ecosystem(ecosystem).Valid() {
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
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("could not determine working directory: %w", err)
	}

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
	ctx, stop := commandContext(cmd)
	defer stop()
	result, err := operation.Remove(ctx, workDir, args, ecosystem, opts)
	if err != nil {
		return output.Failure(err)
	}
	return output.Render(result)
}
