package cli

import "github.com/spf13/cobra"

// Output style flag names. The whole family is registered once as root
// persistent flags and applies to every subcommand.
const (
	FlagJSON            = "json"
	FlagJSONCompact     = "json-compact"
	FlagLong            = "long"
	FlagNoColor         = "no-color"
	FlagNoInteractive   = "no-interactive"
	FlagNoStyle         = "no-style"
	FlagLogo            = "logo"
	FlagLogFile         = "log-file"
	FlagPrintLogs       = "print-logs"
	FlagDebug           = "debug"
	FlagDumpLogs        = "dump-logs"
	FlagPlatform        = "platform"
	FlagUseGitHubMirror = "use-github-mirror"
)

// JSONRendering names the machine-readable rendering of a command result.
type JSONRendering int

const (
	// JSONNone renders the human-facing view.
	JSONNone JSONRendering = iota
	// JSONPretty prints JSON indented with two spaces.
	JSONPretty
	// JSONCompact prints JSON on a single line.
	JSONCompact
)

// JSONMode reports the JSON rendering a command's flags select.
// --json-compact wins over --json when both are set.
func JSONMode(cmd *cobra.Command) JSONRendering {
	if compact, _ := cmd.Flags().GetBool(FlagJSONCompact); compact {
		return JSONCompact
	}
	if pretty, _ := cmd.Flags().GetBool(FlagJSON); pretty {
		return JSONPretty
	}
	return JSONNone
}

func AddLongFlag(cmd *cobra.Command) {
	cmd.Flags().BoolP(FlagLong, "l", false, "Show hidden or collapsed output")
}

func AddLogoFlag(cmd *cobra.Command) {
	cmd.Flags().String(
		FlagLogo,
		"small",
		"Status ASCII logo: none, small, or large (large is opt-in)",
	)
}

// AddPlatformFlag adds an optional target ecosystem selector to a command.
func AddPlatformFlag(cmd *cobra.Command) {
	cmd.Flags().String(FlagPlatform, "", "Resolve packages for PLATFORM")
}
