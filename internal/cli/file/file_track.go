package file

import (
	"github.com/mclucy/lucy/filetrack"

	"github.com/spf13/cobra"
)

var trackCmd = &cobra.Command{
	Use:   "track <path>...",
	Short: TrackShort,
	Long:  TrackLong,
	Args:  cobra.MinimumNArgs(1),
	RunE:  actionFileTrack,
}

var untrackCmd = &cobra.Command{
	Use:   "untrack <path>...",
	Short: UntrackShort,
	Long:  UntrackLong,
	Args:  cobra.MinimumNArgs(1),
	RunE:  actionFileUntrack,
}

var lockCmd = &cobra.Command{
	Use:   "lock <path>...",
	Short: LockShort,
	Long:  LockLong,
	Args:  cobra.MinimumNArgs(1),
	RunE:  actionFileLock,
}

var unlockCmd = &cobra.Command{
	Use:   "unlock <path>...",
	Short: UnlockShort,
	Long:  UnlockLong,
	Args:  cobra.MinimumNArgs(1),
	RunE:  actionFileUnlock,
}

func init() {
	trackCmd.Flags().Bool(
		flagImmutable,
		false,
		"Warn about any change to the tracked contents",
	)
	trackCmd.Flags().Bool(
		flagNoHash,
		false,
		"Track presence only, without an accepted hash",
	)
	trackCmd.Flags().String(
		flagRecoverURL,
		"",
		"Record an explicit recovery source URL for the file",
	)
}

func actionFileTrack(cmd *cobra.Command, args []string) error {
	root, err := workRoot()
	if err != nil {
		return err
	}

	immutable, _ := cmd.Flags().GetBool(flagImmutable)
	noHash, _ := cmd.Flags().GetBool(flagNoHash)
	recoverURL, _ := cmd.Flags().GetString(flagRecoverURL)

	result, err := filetrack.Run(
		cmd.Context(),
		root,
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
	return reportFileResult(cmd, result)
}

func actionFileUntrack(cmd *cobra.Command, args []string) error {
	root, err := workRoot()
	if err != nil {
		return err
	}

	result, err := filetrack.Run(
		cmd.Context(),
		root,
		filetrack.Request{
			Operation: filetrack.Untrack,
			Paths:     args,
		},
	)
	if err != nil {
		return err
	}
	return reportFileResult(cmd, result)
}

func actionFileLock(cmd *cobra.Command, args []string) error {
	root, err := workRoot()
	if err != nil {
		return err
	}

	result, err := filetrack.Run(
		cmd.Context(),
		root,
		filetrack.Request{
			Operation: filetrack.Lock,
			Paths:     args,
		},
	)
	if err != nil {
		return err
	}
	return reportFileResult(cmd, result)
}

func actionFileUnlock(cmd *cobra.Command, args []string) error {
	root, err := workRoot()
	if err != nil {
		return err
	}

	result, err := filetrack.Run(
		cmd.Context(),
		root,
		filetrack.Request{
			Operation: filetrack.Unlock,
			Paths:     args,
		},
	)
	if err != nil {
		return err
	}
	return reportFileResult(cmd, result)
}
