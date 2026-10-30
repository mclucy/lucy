package cli

import "github.com/spf13/cobra"

// HelpOnlyAnnotation marks a command that displays its own help when invoked
// without a subcommand.
const HelpOnlyAnnotation = "lucy.io/help-only"

// MarkHelpOnly records that command only displays help when invoked bare. An
// invocation of it performs no work, so telemetry records the event without
// uploading.
func MarkHelpOnly(command *cobra.Command) {
	if command.Annotations == nil {
		command.Annotations = map[string]string{}
	}
	command.Annotations[HelpOnlyAnnotation] = "true"
}

// IsHelpOnly reports whether command was marked by MarkHelpOnly.
func IsHelpOnly(command *cobra.Command) bool {
	return command != nil && command.Annotations[HelpOnlyAnnotation] != ""
}
