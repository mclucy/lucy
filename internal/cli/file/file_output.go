package file

import (
	"fmt"
	"io"
	"os"

	"github.com/mclucy/lucy/filetrack"
	"github.com/mclucy/lucy/internal/cli"
	"github.com/mclucy/lucy/terminal/style"

	"github.com/spf13/cobra"
)

func workRoot() (string, error) {
	root, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get working directory: %w", err)
	}
	return root, nil
}

type fileResultView struct {
	Operation filetrack.Operation `json:"operation"`
	Changes   []filetrack.Change  `json:"changes,omitempty"`
	Skipped   []string            `json:"skipped,omitempty"`
	Reclaimed filetrack.Reclaimed `json:"reclaimed,omitzero"`
}

var fileResultJSON cli.JSONResult[fileResultView]

func newFileResultView(result filetrack.Result) fileResultView {
	return fileResultView{
		Operation: result.Event.Operation,
		Changes:   result.Event.Changes,
		Skipped:   result.Skipped,
		Reclaimed: result.Reclaimed,
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
	output := cli.NewOperationOutput(cmd)
	if output.JSON() {
		return fileResultJSON.Write(output, newFileResultView(result))
	}

	out := output.Human()
	for _, change := range result.Event.Changes {
		writeChangeLine(out, result.Event.Operation, change)
	}
	for _, skipped := range result.Skipped {
		fmt.Fprintf(out, "skipped %s\n", skipped)
	}
	if result.Reclaimed.Objects > 0 {
		noun := "objects"
		if result.Reclaimed.Objects == 1 {
			noun = "object"
		}
		fmt.Fprintf(
			out,
			"reclaimed %d %s (%s)\n",
			result.Reclaimed.Objects,
			noun,
			style.FormatBytesBinary(result.Reclaimed.Bytes),
		)
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
// replaces every positional path instead of combining with them.
func pathOrAllArgs(cmd *cobra.Command, args []string) error {
	all, _ := cmd.Flags().GetBool(flagAll)
	if all {
		if len(args) > 0 {
			return fmt.Errorf("--%s conflicts with positional paths", flagAll)
		}
		return nil
	}
	if len(args) == 0 {
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
