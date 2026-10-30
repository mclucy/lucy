package cli

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/mclucy/lucy/report"
	"github.com/mclucy/lucy/resolve"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	// ErrorSchema versions the machine-readable failure document.
	ErrorSchema = 1
	// ErrorKind identifies a machine-readable failure document.
	ErrorKind = "error"

	// CodeCommandFailed marks an action that failed for an unclassified
	// reason.
	CodeCommandFailed = "command.failed"
	// CodeInvalidInvocation marks a command line that could not be parsed or
	// validated before the action ran.
	CodeInvalidInvocation = "usage.invalid_invocation"
	// CodeOperationCancelled marks an action interrupted through its context.
	CodeOperationCancelled = "operation.cancelled"
	// CodeResolveNoMatchingBuild marks a version request with no matching
	// builds.
	CodeResolveNoMatchingBuild = "resolve.no_matching_build"
	// CodeResolveNoCompatibleBuild marks a project whose builds do not fit
	// this server.
	CodeResolveNoCompatibleBuild = "resolve.no_compatible_build"
)

// ErrorDocument is the single document a failure writes to stderr in machine
// mode.
type ErrorDocument struct {
	Schema  int      `json:"schema"`
	Kind    string   `json:"kind"`
	Code    string   `json:"code"`
	Title   string   `json:"title"`
	Details []string `json:"details"`
	Hint    string   `json:"hint,omitempty"`
	// Facts carries the machine-readable evidence of the error tree. Human
	// rendering ignores it; the JSON document and the telemetry report body
	// read it, so both describe a failure with the same data.
	Facts map[string]any `json:"facts,omitempty"`
	Log   string         `json:"log"`
}

// SetJSONRendering overrides the rendering selected when the output was
// created. The command boundary uses it when flags fail before parsing.
func (o *Output) SetJSONRendering(mode JSONRendering) {
	o.json = mode
}

// JSONModeFromArgs finds output flags without mistaking another flag's value
// for an output option. It also works when Cobra stops parsing on an error.
func JSONModeFromArgs(cmd *cobra.Command, args []string) JSONRendering {
	pretty, compact := false, false
	flagSets := [3]*pflag.FlagSet{cmd.Flags(), cmd.InheritedFlags(), cmd.PersistentFlags()}
	findFlag := func(name string, shorthand bool) *pflag.Flag {
		for _, flags := range flagSets {
			if shorthand {
				if flag := flags.ShorthandLookup(name); flag != nil {
					return flag
				}
			} else if flag := flags.Lookup(name); flag != nil {
				return flag
			}
		}
		return nil
	}
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			break
		}
		if name, long := strings.CutPrefix(arg, "--"); long {
			name, value, assigned := strings.Cut(name, "=")
			flag := findFlag(name, false)
			if flag != nil && flag.NoOptDefVal == "" && !assigned {
				index++
				continue
			}
			if !assigned {
				value = "true"
			}
			switch name {
			case FlagJSON:
				pretty = boolValue(value)
			case FlagJSONCompact:
				compact = boolValue(value)
			}
			continue
		}
		if shorthand, short := strings.CutPrefix(arg, "-"); short {
			for len(shorthand) > 0 {
				flag := findFlag(shorthand[:1], true)
				if flag == nil {
					break
				}
				shorthand = shorthand[1:]
				if flag.NoOptDefVal == "" {
					if shorthand == "" {
						index++
					}
					break
				}
				if strings.HasPrefix(shorthand, "=") {
					break
				}
			}
		}
	}
	if compact {
		return JSONCompact
	}
	if pretty {
		return JSONPretty
	}
	return JSONNone
}

func boolValue(value string) bool {
	parsed, err := strconv.ParseBool(value)
	return err == nil && parsed
}

// IsUsageError reports failures that happen before a command's own action:
// unknown commands or flags, missing flag values, and rejected arguments.
func IsUsageError(err error) bool {
	if err == nil {
		return false
	}
	if isUsageMessage(err.Error()) {
		return true
	}
	switch unwrapped := err.(type) {
	case interface{ Unwrap() []error }:
		for _, child := range unwrapped.Unwrap() {
			if IsUsageError(child) {
				return true
			}
		}
	case interface{ Unwrap() error }:
		return IsUsageError(unwrapped.Unwrap())
	}
	return false
}

func isUsageMessage(message string) bool {
	for _, prefix := range []string{
		"flag needs an argument:",
		"unknown flag:",
		"unknown shorthand flag:",
		"unknown command",
		"invalid argument",
		"accepts ",
		"requires at least ",
		"required flag(s)",
	} {
		if strings.HasPrefix(message, prefix) {
			return true
		}
	}
	return false
}

// IsFrameworkUsageError reports the subset of invalid invocations that the
// command framework renders with its own usage hint.
func IsFrameworkUsageError(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	for _, prefix := range []string{
		"flag needs an argument:",
		"unknown flag:",
		"unknown shorthand flag:",
		"unknown command",
		"invalid argument",
	} {
		if strings.HasPrefix(message, prefix) {
			return true
		}
	}
	return false
}

// UsageHint names the flags this command accepts when the framework rejected
// an unknown flag or shorthand before the command ran. Mistaking a sibling
// command's flag for this one is the common failure, and the flag list answers
// it without a second round trip to --help. It returns an empty string for
// every other failure.
func UsageHint(cmd *cobra.Command, err error) string {
	if _, ok := unknownFlagName(err); !ok {
		return ""
	}
	names := localFlagNames(cmd)
	if len(names) == 0 {
		return ""
	}
	return cmd.CommandPath() + " accepts " + strings.Join(names, ", ") + "."
}

// unknownFlagName reports the rejected flag, spelled as the caller wrote it.
func unknownFlagName(err error) (string, bool) {
	if err == nil {
		return "", false
	}
	message := err.Error()
	if name, ok := strings.CutPrefix(message, "unknown flag: "); ok {
		return name, true
	}
	rest, ok := strings.CutPrefix(message, "unknown shorthand flag: ")
	if !ok || len(rest) < 3 || rest[0] != '\'' {
		return "", false
	}
	end := strings.Index(rest[1:], "'")
	if end <= 0 {
		return "", false
	}
	return "-" + rest[1:1+end], true
}

// localFlagNames lists the flags this command answers to, sorted for a stable
// message: everything it declares itself, plus the persistent flags of the
// command groups above it, such as `lucy config --global`. The root's
// persistent set stays out because it is identical for every command and does
// not explain this rejection, and so do the framework's --help and --version.
func localFlagNames(cmd *cobra.Command) []string {
	if cmd == nil {
		return nil
	}
	collected := map[string]bool{}
	collect := func(flags *pflag.FlagSet) {
		flags.VisitAll(func(flag *pflag.Flag) {
			switch flag.Name {
			case "help", "version":
				return
			}
			if flag.Hidden {
				return
			}
			collected["--"+flag.Name] = true
		})
	}
	collect(cmd.LocalNonPersistentFlags())
	if root := cmd.Root(); cmd != root {
		collect(cmd.PersistentFlags())
		for parent := cmd.Parent(); parent != nil && parent != root; parent = parent.Parent() {
			collect(parent.PersistentFlags())
		}
	}
	names := make([]string, 0, len(collected))
	for name := range collected {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// WriteError renders one failure. Machine mode writes a single document to
// stderr; human mode keeps the structured detail formatting and names the log
// file. The command boundary calls it exactly once per failure.
func (o *Output) WriteError(err error, logPath string) {
	if err == nil || o == nil || o.cmd == nil {
		return
	}
	if o.JSON() {
		_ = o.writeJSON(o.cmd.ErrOrStderr(), newErrorDocument(o.cmd, err, logPath))
		return
	}
	o.writeHumanError(err, logPath)
}

func (o *Output) writeHumanError(err error, logPath string) {
	writer := o.cmd.ErrOrStderr()
	var detail *report.Detail
	if errors.As(err, &detail) && detail != nil {
		report.RenderDetail(writer, detail, o.inv.Theme)
	} else if IsUsageError(err) {
		fmt.Fprintln(writer, o.inv.Theme.Failure("error:")+" "+err.Error())
		hint := UsageHint(o.cmd, err)
		if hint == "" {
			hint = "Run --help for usage."
		}
		fmt.Fprintln(writer, "  "+o.inv.Theme.Note("hint:")+" "+hint)
	} else {
		report.New(writer, o.inv.Theme).Error(err)
	}
	if logPath != "" {
		fmt.Fprintf(writer, "  %s %s\n", o.inv.Theme.Muted("log:"), logPath)
	}
}

func newErrorDocument(cmd *cobra.Command, err error, logPath string) ErrorDocument {
	document := ErrorDocument{
		Schema:  ErrorSchema,
		Kind:    ErrorKind,
		Details: []string{},
		Log:     logPath,
	}
	detail := errorDetail(err)
	if detail != nil {
		document.Title = plain(detail.Title)
		for _, line := range detail.Lines {
			document.Details = append(document.Details, plain(line))
		}
		document.Hint = plain(detail.Hint)
		document.Code = detail.Code
		if document.Title == "" && detail.Cause != nil {
			document.Title = plain(detail.Cause.Error())
		}
		document.Details = append(document.Details, joinedDetails(err, detail)...)
	} else {
		lines := errorLines(err)
		if len(lines) > 0 {
			document.Title = lines[0]
			document.Details = append(document.Details, lines[1:]...)
		}
	}
	if document.Title == "" {
		document.Title = "command failed"
	}
	if document.Code == "" {
		document.Code = errorCode(err)
	}
	if document.Hint == "" && document.Code == CodeInvalidInvocation {
		document.Hint = UsageHint(cmd, err)
		if document.Hint == "" {
			document.Hint = "Run --help for usage."
		}
	}
	document.Facts = FailureFacts(err)
	return document
}

func errorDetail(err error) *report.Detail {
	var detail *report.Detail
	if errors.As(err, &detail) && detail != nil {
		return detail
	}
	var failure *resolve.Failure
	if errors.As(err, &failure) && failure != nil {
		return resolutionDetail(failure)
	}
	return nil
}

// joinedDetails keeps secondary failures, such as a log file that fails to
// close, inside the primary failure's document instead of dropping them or
// writing a second document.
func joinedDetails(err error, primary *report.Detail) []string {
	joined, ok := err.(interface{ Unwrap() []error })
	if !ok {
		return nil
	}
	var lines []string
	for _, child := range joined.Unwrap() {
		var detail *report.Detail
		if errors.As(child, &detail) && detail == primary {
			continue
		}
		if primary.Cause != nil && errors.Is(child, primary.Cause) {
			continue
		}
		lines = append(lines, errorLines(child)...)
	}
	return lines
}

func errorLines(err error) []string {
	raw := strings.Split(plain(err.Error()), "\n")
	lines := make([]string, 0, len(raw))
	for _, line := range raw {
		line = strings.TrimSuffix(line, "\r")
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

func errorCode(err error) string {
	switch {
	case IsUsageError(err):
		return CodeInvalidInvocation
	case errors.Is(err, context.Canceled),
		errors.Is(err, context.DeadlineExceeded):
		return CodeOperationCancelled
	}
	var failure *resolve.Failure
	if errors.As(err, &failure) && failure != nil {
		if failure.Aborted {
			return CodeCommandFailed
		}
		if len(failure.Groups) == 1 &&
			failure.Groups[0].Kind == resolve.RejectionVersionMismatch {
			return CodeResolveNoMatchingBuild
		}
		return CodeResolveNoCompatibleBuild
	}
	return CodeCommandFailed
}

func plain(value string) string {
	return ansi.Strip(value)
}
