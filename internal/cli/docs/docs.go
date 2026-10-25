package docs

import (
	"github.com/mclucy/lucy/internal/cli/help"
	"github.com/spf13/cobra"
)

// NewCommand returns the hidden documentation command.
func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "docs",
		Short:  help.DocsShort,
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(
		newListCommand(),
		newGrepCommand(),
		newViewCommand(),
	)
	return cmd
}
