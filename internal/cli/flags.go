package cli

import "github.com/spf13/cobra"

// Shared flag names. Root persistent flags and per-command local flags must
// agree on these strings.
const (
	FlagJSON        = "json"
	FlagJSONCompact = "json-compact"
	FlagLong        = "long"
	FlagNoStyle     = "no-style"
	FlagLogFile     = "log-file"
	FlagPrintLogs   = "print-logs"
	FlagDebug       = "debug"
	FlagDumpLogs    = "dump-logs"
)

// AddJSONFlag adds the --json flag to a command.
func AddJSONFlag(cmd *cobra.Command) {
	cmd.Flags().Bool(FlagJSON, false, "Print raw JSON response")
}

func AddJSONCompactFlag(cmd *cobra.Command) {
	cmd.Flags().Bool(FlagJSONCompact, false, "Print raw JSON response without indentation")
}

// AddLongFlag adds the --long/-l flag to a command.
func AddLongFlag(cmd *cobra.Command) {
	cmd.Flags().BoolP(FlagLong, "l", false, "Show hidden or collapsed output")
}

// AddNoStyleFlag adds the --no-style flag to a command (local, not persistent).
func AddNoStyleFlag(cmd *cobra.Command) {
	cmd.Flags().Bool(FlagNoStyle, false, "Disable colored and styled output")
}
