package file

import (
	"fmt"

	"github.com/mclucy/lucy/filetrack"

	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:   "update [path...]",
	Short: UpdateShort,
	Long:  UpdateLong,
	Args:  pathOrAllArgs,
	RunE:  actionFileUpdate,
}

var recoverCmd = &cobra.Command{
	Use:   "recover [path...]",
	Short: RecoverShort,
	Long:  RecoverLong,
	Args:  pathOrAllArgs,
	RunE:  actionFileRecover,
}

var revertCmd = &cobra.Command{
	Use:   "revert <path> [hash]",
	Short: RevertShort,
	Long:  RevertLong,
	Args:  revertArgs,
	RunE:  actionFileRevert,
}

func init() {
	updateCmd.Flags().Bool(
		flagFromSource,
		false,
		"Fetch fresh bytes from recover_url instead of the working file",
	)
	updateCmd.Flags().Bool(
		flagAll,
		false,
		"Update every tracked file atomically",
	)

	recoverCmd.Flags().Bool(
		flagAll,
		false,
		"Restore every tracked file with an accepted hash atomically",
	)

	revertCmd.Flags().Uint64(
		flagRevision,
		0,
		"Restore one exact historical revision sequence",
	)
}

func actionFileUpdate(cmd *cobra.Command, args []string) error {
	root, err := workRoot()
	if err != nil {
		return err
	}

	fromSource, _ := cmd.Flags().GetBool(flagFromSource)
	all, _ := cmd.Flags().GetBool(flagAll)

	result, err := filetrack.Run(
		cmd.Context(),
		root,
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
	return reportFileResult(cmd, result)
}

func actionFileRecover(cmd *cobra.Command, args []string) error {
	root, err := workRoot()
	if err != nil {
		return err
	}

	all, _ := cmd.Flags().GetBool(flagAll)

	result, err := filetrack.Run(
		cmd.Context(),
		root,
		filetrack.Request{
			Operation: filetrack.Recover,
			Paths:     args,
			All:       all,
		},
	)
	if err != nil {
		return err
	}
	return reportFileResult(cmd, result)
}

func actionFileRevert(cmd *cobra.Command, args []string) error {
	root, err := workRoot()
	if err != nil {
		return err
	}

	hash := ""
	if len(args) == 2 {
		hash = args[1]
	}
	revision, _ := cmd.Flags().GetUint64(flagRevision)
	if cmd.Flags().Changed(flagRevision) && revision == 0 {
		return fmt.Errorf("--revision requires a positive history sequence")
	}

	result, err := filetrack.Run(
		cmd.Context(),
		root,
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
	return reportFileResult(cmd, result)
}
