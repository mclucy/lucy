package cli

import (
	"strings"

	"github.com/spf13/cobra"
)

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
	FlagLogLevel        = "log-level"
	FlagEcosystem       = "ecosystem"
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

// JSONFlagsPresent reports whether the raw command line names a JSON flag.
// The parser is not involved, so this works before flag parsing fails.
func JSONFlagsPresent(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if arg == "--json" ||
			strings.HasPrefix(arg, "--json=") ||
			arg == "--json-compact" ||
			strings.HasPrefix(arg, "--json-compact=") {
			return true
		}
	}
	return false
}

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

// AddEcosystemFlag adds an optional mod or plugin type to a command.
func AddEcosystemFlag(cmd *cobra.Command) {
	cmd.Flags().String(FlagEcosystem, "", "Mod or plugin type (fabric, forge, neoforge, bukkit, paper, mcdr)")
}
