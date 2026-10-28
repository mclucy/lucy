package file

import (
	"fmt"

	"github.com/mclucy/lucy/filetrack"
	"github.com/mclucy/lucy/internal/cli"

	"github.com/spf13/cobra"
)

func newUpdateCmd(
	inv *cli.Invocation,
	deps *cli.Dependencies,
) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update [path...]",
		Short: UpdateShort,
		Long:  UpdateLong,
		Args:  pathOrAllArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return actionFileUpdate(cmd, inv, deps, args)
		},
	}
	cmd.Flags().Bool(
		flagFromSource,
		false,
		"Fetch fresh bytes from recover_url instead of the working file",
	)
	cmd.Flags().Bool(
		flagAll,
		false,
		"Update every tracked file atomically",
	)
	return cmd
}

func newRecoverCmd(
	inv *cli.Invocation,
	deps *cli.Dependencies,
) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "recover [path...]",
		Short: RecoverShort,
		Long:  RecoverLong,
		Args:  pathOrAllArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return actionFileRecover(cmd, inv, deps, args)
		},
	}
	cmd.Flags().Bool(
		flagAll,
		false,
		"Restore every tracked file with an accepted hash atomically",
	)
	return cmd
}

func newRevertCmd(
	inv *cli.Invocation,
	deps *cli.Dependencies,
) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "revert <path> [hash]",
		Short: RevertShort,
		Long:  RevertLong,
		Args:  revertArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return actionFileRevert(cmd, inv, deps, args)
		},
	}
	cmd.Flags().Uint64(
		flagRevision,
		0,
		"Restore one exact historical revision sequence",
	)
	return cmd
}

func actionFileUpdate(
	cmd *cobra.Command,
	inv *cli.Invocation,
	deps *cli.Dependencies,
	args []string,
) error {
	fromSource, _ := cmd.Flags().GetBool(flagFromSource)
	all, _ := cmd.Flags().GetBool(flagAll)

	result, err := deps.Files.Run(
		cmd.Context(),
		inv.Directory,
		filetrack.Request{
			Operation:  filetrack.Update,
			Paths:      args,
			All:        all,
			FromSource: fromSource,
		},
	)
	if err != nil {
		return err
	}
	return reportFileResult(cmd, inv, result)
}

func actionFileRecover(
	cmd *cobra.Command,
	inv *cli.Invocation,
	deps *cli.Dependencies,
	args []string,
) error {
	all, _ := cmd.Flags().GetBool(flagAll)

	result, err := deps.Files.Run(
		cmd.Context(),
		inv.Directory,
		filetrack.Request{
			Operation: filetrack.Recover,
			Paths:     args,
			All:       all,
		},
	)
	if err != nil {
		return err
	}
	return reportFileResult(cmd, inv, result)
}

func actionFileRevert(
	cmd *cobra.Command,
	inv *cli.Invocation,
	deps *cli.Dependencies,
	args []string,
) error {
	hash := ""
	if len(args) == 2 {
		hash = args[1]
	}
	revision, _ := cmd.Flags().GetUint64(flagRevision)
	if cmd.Flags().Changed(flagRevision) && revision == 0 {
		return fmt.Errorf("--revision requires a positive history sequence")
	}

	result, err := deps.Files.Run(
		cmd.Context(),
		inv.Directory,
		filetrack.Request{
			Operation: filetrack.Revert,
			Paths:     args[:1],
			Hash:      hash,
			Revision:  revision,
		},
	)
	if err != nil {
		return err
	}
	return reportFileResult(cmd, inv, result)
}
