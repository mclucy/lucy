package docs

import (
	"fmt"

	docscontent "github.com/mclucy/lucy/docs"
	"github.com/mclucy/lucy/internal/cli/help"
	"github.com/spf13/cobra"
)

func newListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: help.DocsListShort,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			pages, err := docscontent.Load()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			for _, page := range pages {
				if _, err := fmt.Fprintln(out, page.Slug); err != nil {
					return err
				}
			}
			return nil
		},
	}
}
