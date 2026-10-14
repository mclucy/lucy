package file

import (
	"fmt"

	"github.com/mclucy/lucy/filetrack"
	"github.com/mclucy/lucy/report"

	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   ListShort,
	Long:    ListLong,
	Args:    cobra.NoArgs,
	RunE:    actionFileList,
}

func actionFileList(cmd *cobra.Command, _ []string) error {
	root, err := workRoot()
	if err != nil {
		return err
	}

	files, err := filetrack.DeclaredFiles(root)
	if err != nil {
		return err
	}

	statuses, err := filetrack.Inspect(root, files)
	if err != nil {
		return err
	}
	if statuses == nil {
		statuses = []filetrack.Status{}
	}
	for _, status := range statuses {
		if status.Immutable && status.State == "modified" {
			report.Warn(fmt.Errorf("%s: immutable contents differ; use lucy file update to accept them or lucy file recover to restore the accepted revision", status.Path))
		}
	}

	if machineOutputRequested(cmd) {
		return writeJSON(cmd, statuses)
	}

	out := cmd.OutOrStdout()
	if len(statuses) == 0 {
		fmt.Fprintln(out, "No tracked files.")
		return nil
	}

	pathWidth, stateWidth := 0, 0
	for _, status := range statuses {
		pathWidth = max(pathWidth, len(status.Path))
		stateWidth = max(stateWidth, len(status.State))
	}
	for _, status := range statuses {
		fmt.Fprintf(
			out,
			"%-*s  %-9s  %-*s  %s\n",
			pathWidth,
			status.Path,
			statusPolicy(status.Immutable),
			stateWidth,
			status.State,
			statusDetail(status),
		)
	}
	return nil
}

func statusPolicy(immutable bool) string {
	if immutable {
		return "immutable"
	}
	return "mutable"
}

// statusDetail reports the accepted hash behind a state, plus the observed
// hash when it differs and the reason when the state could not be observed.
func statusDetail(status filetrack.Status) string {
	if status.Error != "" {
		return status.Error
	}
	if status.Hash == "" {
		return "no accepted hash"
	}
	if status.ActualHash == "" || status.ActualHash == status.Hash {
		return status.Hash
	}
	return status.Hash + " (actual " + status.ActualHash + ")"
}
