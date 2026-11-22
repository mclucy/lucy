package cli

import "github.com/spf13/cobra"

// HelpOnlyAnnotation marks a command that displays its own help when invoked
// bare.
const HelpOnlyAnnotation = "lucy.io/help-only"

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
