package cli

import (
	"strconv"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// TelemetryExemptAnnotation marks a command tree excluded from automatic
// invocation telemetry.
const TelemetryExemptAnnotation = "lucy.io/telemetry-exempt"

// TelemetryArgumentKindAnnotation marks a flag whose parsed value has a closed,
// safe domain. Unmarked flags are recorded by name and kind only.
const TelemetryArgumentKindAnnotation = "lucy.io/telemetry-argument-kind"

// telemetryPositionArgumentAnnotation marks the semantic domain of one
// positional argument by its 1-based position.
const telemetryPositionArgumentAnnotation = "lucy.io/telemetry-position-argument-kind"

// Semantic value domains that may be recorded. Other values stay local.
const (
	TelemetryArgumentSelector = "selector"
	TelemetryArgumentVersion  = "version"
)

// MarkTelemetryExempt excludes a command tree from automatic invocation
// telemetry. Explicit telemetry operations remain available.
func MarkTelemetryExempt(command *cobra.Command) {
	if command.Annotations == nil {
		command.Annotations = map[string]string{}
	}
	command.Annotations[TelemetryExemptAnnotation] = "true"
}

// MarkTelemetryFlagArgument declares that a flag carries a selector or version
// value that the telemetry boundary may record after validating its grammar.
func MarkTelemetryFlagArgument(command *cobra.Command, name, kind string) {
	if command == nil || !telemetryArgumentKind(kind) {
		return
	}
	flag := command.Flags().Lookup(name)
	if flag == nil {
		return
	}
	if flag.Annotations == nil {
		flag.Annotations = map[string][]string{}
	}
	flag.Annotations[TelemetryArgumentKindAnnotation] = []string{kind}
}

// MarkTelemetryPositionArgument declares the semantic domain of one positional
// argument. Unmarked positions are free text and never record their values.
func MarkTelemetryPositionArgument(command *cobra.Command, position int, kind string) {
	if command == nil || position < 1 || !telemetryArgumentKind(kind) {
		return
	}
	if command.Annotations == nil {
		command.Annotations = map[string]string{}
	}
	command.Annotations[telemetryPositionArgumentKey(position)] = kind
}

// TelemetryFlagArgumentKind returns the declared semantic domain of a flag, or
// an empty string when the flag is free text.
func TelemetryFlagArgumentKind(flag *pflag.Flag) string {
	if flag == nil {
		return ""
	}
	values := flag.Annotations[TelemetryArgumentKindAnnotation]
	if len(values) != 1 || !telemetryArgumentKind(values[0]) {
		return ""
	}
	return values[0]
}

// TelemetryPositionArgumentKind returns the declared semantic domain of a
// positional argument, or an empty string when the position is free text.
func TelemetryPositionArgumentKind(command *cobra.Command, position int) string {
	if command == nil || position < 1 {
		return ""
	}
	kind := command.Annotations[telemetryPositionArgumentKey(position)]
	if !telemetryArgumentKind(kind) {
		return ""
	}
	return kind
}

func telemetryPositionArgumentKey(position int) string {
	return telemetryPositionArgumentAnnotation + "." + strconv.Itoa(position)
}

func telemetryArgumentKind(kind string) bool {
	return kind == TelemetryArgumentSelector || kind == TelemetryArgumentVersion
}

// TelemetryExempt reports whether a command or an ancestor is marked.
func TelemetryExempt(command *cobra.Command) bool {
	for current := command; current != nil; current = current.Parent() {
		if current.Annotations[TelemetryExemptAnnotation] != "" {
			return true
		}
	}
	return false
}
