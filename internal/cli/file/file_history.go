package file

import (
	"fmt"
	"io"
	"time"

	"github.com/mclucy/lucy/filetrack"
	"github.com/mclucy/lucy/internal/cli"

	"github.com/spf13/cobra"
)

var historyCmd = &cobra.Command{
	Use:   "history [path]",
	Short: HistoryShort,
	Long:  HistoryLong,
	Args:  cobra.MaximumNArgs(1),
	RunE:  actionFileHistory,
}

var historyJSON cli.JSONResult[[]filetrack.Event]

func actionFileHistory(cmd *cobra.Command, args []string) error {
	root, err := workRoot()
	if err != nil {
		return err
	}

	path := ""
	if len(args) == 1 {
		path = args[0]
	}

	events, err := filetrack.History(root, path)
	if err != nil {
		return err
	}
	if events == nil {
		events = []filetrack.Event{}
	}

	output := cli.NewQueryOutput(cmd)
	if output.JSON() {
		return historyJSON.Write(output, events)
	}

	out := output.Human()
	if len(events) == 0 {
		fmt.Fprintln(out, "No file tracking history.")
		return nil
	}

	for _, event := range events {
		fmt.Fprintf(
			out,
			"#%d  %s  %s\n",
			event.Sequence,
			event.Time.UTC().Format(time.RFC3339),
			event.Operation,
		)
		for _, change := range event.Changes {
			writeHistoryChange(out, change)
		}
	}
	return nil
}

func writeHistoryChange(out io.Writer, change filetrack.Change) {
	if change.Accepted && change.After != nil && change.After.Hash != "" {
		fmt.Fprintf(
			out,
			"    %s accepted %s as revision %d\n",
			change.Path,
			change.After.Hash,
			change.Revision,
		)
	} else if change.Revision != 0 && change.After != nil {
		fmt.Fprintf(out, "    %s active revision %d: %s\n", change.Path, change.Revision, change.After.Hash)
	}
	if change.BackupHash != "" {
		fmt.Fprintf(
			out,
			"    %s working backup %s\n",
			change.Path,
			change.BackupHash,
		)
	} else if change.Revision == 0 {
		fmt.Fprintf(out, "    %s\n", change.Path)
	}
}
