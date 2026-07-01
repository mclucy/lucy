package cmd

import (
	"github.com/spf13/cobra"
)

const (
	flagJsonName        = "json"
	flagJsonCompactName = "json-compact"
	flagLongName        = "long"
	flagNoStyleName     = "no-style"
	flagLogFileName     = "log-file"
	flagPrintLogsName   = "print-logs"
	flagDebugName       = "debug"
	flagDumpLogsName    = "dump-logs"
)

// addJsonFlag adds the --json flag to a command.
func addJsonFlag(cmd *cobra.Command) {
	cmd.Flags().Bool(flagJsonName, false, "Print raw JSON response")
}

func addJsonCompactFlag(cmd *cobra.Command) {
	cmd.Flags().Bool(flagJsonCompactName, false, "Print raw JSON response without indentation")
}

// addLongFlag adds the --long/-l flag to a command.
func addLongFlag(cmd *cobra.Command) {
	cmd.Flags().BoolP(flagLongName, "l", false, "Show hidden or collapsed output")
}

// addNoStyleFlag adds the --no-style flag to a command (local, not persistent).
func addNoStyleFlag(cmd *cobra.Command) {
	cmd.Flags().Bool(flagNoStyleName, false, "Disable colored and styled output")
}
