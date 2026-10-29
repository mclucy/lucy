package file

import (
	"github.com/mclucy/lucy/filetrack"
	"github.com/mclucy/lucy/internal/cli"

	"github.com/spf13/cobra"
)

func newTrackCmd(
	inv *cli.Invocation,
	deps *cli.Dependencies,
) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "track <path>...",
		Short: TrackShort,
		Long:  TrackLong,
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return actionFileTrack(cmd, inv, deps, args)
		},
	}
	cmd.Flags().Bool(
		flagImmutable,
		false,
		"Warn about any change to the tracked contents",
	)
	cmd.Flags().Bool(
		flagNoHash,
		false,
		"Track presence only, without an accepted hash",
	)
	cmd.Flags().String(
		flagRecoverURL,
		"",
		"Record an explicit recovery source URL for the file",
	)
	return cmd
}

func newUntrackCmd(
	inv *cli.Invocation,
	deps *cli.Dependencies,
) *cobra.Command {
	return &cobra.Command{
		Use:   "untrack <path>...",
		Short: UntrackShort,
		Long:  UntrackLong,
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return actionFileUntrack(cmd, inv, deps, args)
		},
	}
}

func newLockCmd(
	inv *cli.Invocation,
	deps *cli.Dependencies,
) *cobra.Command {
	return &cobra.Command{
		Use:   "lock <path>...",
		Short: LockShort,
		Long:  LockLong,
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return actionFileLock(cmd, inv, deps, args)
		},
	}
}

func newUnlockCmd(
	inv *cli.Invocation,
	deps *cli.Dependencies,
) *cobra.Command {
	return &cobra.Command{
		Use:   "unlock <path>...",
		Short: UnlockShort,
		Long:  UnlockLong,
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return actionFileUnlock(cmd, inv, deps, args)
		},
	}
}

func actionFileTrack(
	cmd *cobra.Command,
	inv *cli.Invocation,
	deps *cli.Dependencies,
	args []string,
) error {
	immutable, _ := cmd.Flags().GetBool(flagImmutable)
	noHash, _ := cmd.Flags().GetBool(flagNoHash)
	recoverURL, _ := cmd.Flags().GetString(flagRecoverURL)

	result, err := deps.Files.Run(
		cmd.Context(),
		inv.Directory,
		filetrack.Request{
			Operation:  filetrack.Track,
			Paths:      args,
			Immutable:  immutable,
			NoHash:     noHash,
			RecoverURL: recoverURL,
		},
	)
	if err != nil {
		return err
	}
	return reportFileResult(cmd, inv, result)
}

func actionFileUntrack(
	cmd *cobra.Command,
	inv *cli.Invocation,
	deps *cli.Dependencies,
	args []string,
) error {
	result, err := deps.Files.Run(
		cmd.Context(),
		inv.Directory,
		filetrack.Request{
			Operation: filetrack.Untrack,
			Paths:     args,
		},
	)
	if err != nil {
		return err
	}
	return reportFileResult(cmd, inv, result)
}

func actionFileLock(
	cmd *cobra.Command,
	inv *cli.Invocation,
	deps *cli.Dependencies,
	args []string,
) error {
	result, err := deps.Files.Run(
		cmd.Context(),
		inv.Directory,
		filetrack.Request{
			Operation: filetrack.Lock,
			Paths:     args,
		},
	)
	if err != nil {
		return err
	}
	return reportFileResult(cmd, inv, result)
}

func actionFileUnlock(
	cmd *cobra.Command,
	inv *cli.Invocation,
	deps *cli.Dependencies,
	args []string,
) error {
	result, err := deps.Files.Run(
		cmd.Context(),
		inv.Directory,
		filetrack.Request{
			Operation: filetrack.Unlock,
			Paths:     args,
		},
	)
	if err != nil {
		return err
	}
	return reportFileResult(cmd, inv, result)
}
