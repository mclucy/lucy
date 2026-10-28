// Package file adapts the tracked-file command surface onto cobra.
//
// Every subcommand forwards its explicit choices to the filetrack service and
// prints the result; the adapter holds no tracking policy, digest math, or
// content-address storage logic of its own.
package file

import (
	"github.com/mclucy/lucy/internal/cli"
	"github.com/spf13/cobra"
)

// Flag names of the file subcommands. Output flags come from the root
// persistent flags and are read through their shared constants.
const (
	flagAll        = "all"
	flagFromSource = "from-source"
	flagImmutable  = "immutable"
	flagNoHash     = "no-hash"
	flagRecoverURL = "recover-url"
	flagRevision   = "revision"
)

const (
	FileShort = "Track files, accept revisions, and restore recorded content"

	TrackShort   = "Start tracking an existing file"
	UntrackShort = "Stop tracking a file"
	UpdateShort  = "Accept current or fresh contents as the accepted revision"
	RecoverShort = "Restore a file to its accepted contents"
	RevertShort  = "Restore and accept a historical revision"
	LockShort    = "Accept current contents and mark the file immutable"
	UnlockShort  = "Stop warning about changes to a file"
	ListShort    = "Show the state of tracked files"
	HistoryShort = "List tracked file events and revisions"
)

const (
	FileLong = "Run from a server workspace. Paths are relative to the " +
		"directory containing lucy.yaml."

	TrackLong = "Snapshot the current contents of existing regular files " +
		"into the local content store and declare them in lucy.yaml. One " +
		"invocation accepts every path atomically. By default each entry " +
		"is mutable with an accepted hash; --immutable requires an " +
		"accepted hash and raises warnings on deviation, --no-hash tracks " +
		"presence only, and --recover-url records an explicit recovery " +
		"source. An already tracked path fails rather than being " +
		"implicitly updated."

	UntrackLong = "Remove the declarations from lucy.yaml. Working content " +
		"and historical backups are preserved."

	UpdateLong = "Snapshot and accept current bytes for one or more tracked " +
		"files, or accept every tracked file at once with --all. The " +
		"change policy and recover_url stay unchanged. --from-source " +
		"fetches fresh bytes from recover_url, preserves the displaced " +
		"working bytes, and accepts the new digest without requiring a " +
		"match on the old one."

	RecoverLong = "Restore one or more tracked files to their accepted " +
		"digests without accepting a new revision. Local content-addressed blobs are " +
		"verified first; when none is available the recover_url is " +
		"fetched directly and checked against the accepted digest. " +
		"Displaced working bytes are preserved without being accepted. " +
		"--all restores every entry with an accepted hash and reports " +
		"presence-only entries as skipped."

	RevertLong = "Restore and accept a backed-up historical revision of a " +
		"tracked file. Without a target the accepted revision's parent is " +
		"followed, so repeated reverts navigate backward instead of " +
		"toggling. A hash selects a digest, --revision selects one exact " +
		"historical sequence."

	LockLong = "Snapshot and accept the current bytes of one or more files, " +
		"then warn about any future deviation without restoring or " +
		"replacing them."

	UnlockLong = "Accept the existing hashes and recover_urls but stop " +
		"warning about deviation."

	ListLong = "Read-only inspection of every tracked file. Mutable files " +
		"may deviate without warning; immutable files warn during " +
		"installation."

	HistoryLong = "List file tracking events in chronological order with " +
		"their sequence number, timestamp, digest, and operation. An " +
		"optional path restricts the listing to one file, including one " +
		"that is no longer tracked. No network access happens."
)

// NewCommand returns the file command tree. Output flags are inherited from
// the root command and are never registered again here.
func NewCommand(contracts *cli.JSONContracts) *cobra.Command {
	trackCmd := newTrackCmd()
	untrackCmd := newUntrackCmd()
	updateCmd := newUpdateCmd()
	recoverCmd := newRecoverCmd()
	revertCmd := newRevertCmd()
	lockCmd := newLockCmd()
	unlockCmd := newUnlockCmd()
	listCmd := newListCmd()
	historyCmd := newHistoryCmd()

	fileResultJSON.Bind(contracts, trackCmd)
	fileResultJSON.Bind(contracts, untrackCmd)
	fileResultJSON.Bind(contracts, updateCmd)
	fileResultJSON.Bind(contracts, recoverCmd)
	fileResultJSON.Bind(contracts, revertCmd)
	fileResultJSON.Bind(contracts, lockCmd)
	fileResultJSON.Bind(contracts, unlockCmd)
	listJSON.Bind(contracts, listCmd)
	historyJSON.Bind(contracts, historyCmd)

	fileCmd := &cobra.Command{
		Use:   "file <subcommand>",
		Short: FileShort,
		Long:  FileLong,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	fileCmd.AddCommand(
		trackCmd,
		untrackCmd,
		updateCmd,
		recoverCmd,
		revertCmd,
		lockCmd,
		unlockCmd,
		listCmd,
		historyCmd,
	)
	return fileCmd
}
