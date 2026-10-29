package docs

import (
	"fmt"

	"github.com/mclucy/lucy/internal/cli/help"
	"github.com/spf13/cobra"
)

func newListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: help.DocsListShort,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			pages, err := loadPages()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			for _, page := range pages {
				if _, err := fmt.Fprintln(out, page.slug); err != nil {
					return err
				}
			}
			return nil
		},
	}
}
