package file

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/mclucy/lucy/filetrack"
	"github.com/mclucy/lucy/internal/cli"
	"github.com/mclucy/lucy/lockfile"
	"github.com/mclucy/lucy/manifest"
	"github.com/mclucy/lucy/types"

	"github.com/spf13/cobra"
)

// errNoDeclarations reports a workspace where neither state file exists, so a
// read-only command cannot silently report an empty tracking state.
var errNoDeclarations = fmt.Errorf(
	"no %s or %s in this workspace",
	manifest.Filename,
	lockfile.Filename,
)

// declaredFiles prefers the manifest declarations and falls back to the lock
// only when no manifest exists, so inspection also works in an installed-only
// workspace. A present but unreadable file is always an error.
func declaredFiles(root string) ([]types.TrackedFile, error) {
	doc, manifestErr := manifest.Read(root)
	switch {
	case manifestErr == nil:
		return doc.Files, nil
	case !errors.Is(manifestErr, os.ErrNotExist):
		return nil, fmt.Errorf("read manifest: %w", manifestErr)
	}

	lock, lockErr := lockfile.Read(root)
	switch {
	case lockErr == nil:
		return lock.Files, nil
	case !errors.Is(lockErr, os.ErrNotExist):
		return nil, fmt.Errorf("read lockfile: %w", lockErr)
	}

	return nil, errNoDeclarations
}

func workRoot() (string, error) {
	root, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get working directory: %w", err)
	}
	return root, nil
}

func machineOutputRequested(cmd *cobra.Command) bool {
	json, _ := cmd.Flags().GetBool(cli.FlagJSON)
	jsonCompact, _ := cmd.Flags().GetBool(cli.FlagJSONCompact)
	return json || jsonCompact
}

func writeJSON(cmd *cobra.Command, value any) error {
	compact, _ := cmd.Flags().GetBool(cli.FlagJSONCompact)

	var (
		data []byte
		err  error
	)
	if compact {
		data, err = json.Marshal(value)
	} else {
		data, err = json.Marshal(
			value,
			jsontext.Multiline(true),
			jsontext.WithIndent("  "),
		)
	}
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	if _, err := out.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	return nil
}

type fileResultView struct {
	Operation filetrack.Operation `json:"operation"`
	Changes   []filetrack.Change  `json:"changes,omitempty"`
	Skipped   []string            `json:"skipped,omitempty"`
}

func newFileResultView(result filetrack.Result) fileResultView {
	return fileResultView{
		Operation: result.Event.Operation,
		Changes:   result.Event.Changes,
		Skipped:   result.Skipped,
	}
}

var resultVerbs = map[filetrack.Operation]string{
	filetrack.Track:   "tracked",
	filetrack.Untrack: "untracked",
	filetrack.Update:  "updated",
	filetrack.Recover: "recovered",
	filetrack.Revert:  "reverted",
	filetrack.Lock:    "locked",
	filetrack.Unlock:  "unlocked",
}

func reportFileResult(cmd *cobra.Command, result filetrack.Result) error {
	if machineOutputRequested(cmd) {
		return writeJSON(cmd, newFileResultView(result))
	}

	out := cmd.OutOrStdout()
	for _, change := range result.Event.Changes {
		writeChangeLine(out, result.Event.Operation, change)
	}
	for _, skipped := range result.Skipped {
		fmt.Fprintf(out, "skipped %s\n", skipped)
	}
	return nil
}

func writeChangeLine(
	out io.Writer,
	operation filetrack.Operation,
	change filetrack.Change,
) {
	fmt.Fprintf(out, "%s %s\n", resultVerbs[operation], change.Path)
	switch {
	case change.Accepted && change.After != nil && change.After.Hash != "":
		fmt.Fprintf(out, "  accepted %s as revision %d\n", change.After.Hash, change.Revision)
	case change.Revision != 0 && change.After != nil && change.After.Hash != "":
		fmt.Fprintf(out, "  active revision %d: %s\n", change.Revision, change.After.Hash)
	}
	if displacedBytes(change) {
		fmt.Fprintf(out, "  working backup %s\n", change.BackupHash)
	}
}

// displacedBytes reports whether the working backup holds bytes other than the
// ones the change published. Restoring unchanged content keeps a backup of
// itself, which is not news the user asked for.
func displacedBytes(change filetrack.Change) bool {
	if change.BackupHash == "" {
		return false
	}
	return change.After == nil || change.BackupHash != change.After.Hash
}

// pathOrAllArgs enforces the selection shared by update and recover: --all
// replaces every positional path instead of combining with one.
func pathOrAllArgs(cmd *cobra.Command, args []string) error {
	all, _ := cmd.Flags().GetBool(flagAll)
	if all {
		if len(args) > 0 {
			return fmt.Errorf("--%s conflicts with positional paths", flagAll)
		}
		return nil
	}
	if len(args) != 1 {
		return fmt.Errorf("requires a path argument or --%s", flagAll)
	}
	return nil
}

// revertArgs enforces revert's optional targets: a path may carry one hash,
// and --revision selects a sequence instead of a recurrent digest.
func revertArgs(cmd *cobra.Command, args []string) error {
	if len(args) < 1 || len(args) > 2 {
		return fmt.Errorf("requires a path and an optional hash argument")
	}
	if len(args) == 2 && cmd.Flags().Changed(flagRevision) {
		return fmt.Errorf("--%s conflicts with the hash argument", flagRevision)
	}
	return nil
}
