package cli

import "github.com/spf13/cobra"

// TelemetryExemptAnnotation marks a command tree excluded from automatic
// invocation telemetry.
const TelemetryExemptAnnotation = "lucy.io/telemetry-exempt"

// MarkTelemetryExempt excludes a command tree from automatic invocation
// telemetry. Explicit telemetry operations remain available.
func MarkTelemetryExempt(command *cobra.Command) {
	if command.Annotations == nil {
		command.Annotations = map[string]string{}
	}
	command.Annotations[TelemetryExemptAnnotation] = "true"
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
