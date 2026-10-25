package docs

import (
	"fmt"

	"github.com/mclucy/lucy/internal/cli/help"
	"github.com/spf13/cobra"
)

func newViewCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "view <page>",
		Short: help.DocsViewShort,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug, err := normalizeSlug(args[0])
			if err != nil {
				return err
			}
			pages, err := loadPages()
			if err != nil {
				return err
			}
			selected, ok := findPage(pages, slug)
			if !ok {
				return fmt.Errorf(
					"unknown documentation page %q; run lucy docs list",
					args[0],
				)
			}
			_, err = cmd.OutOrStdout().Write(selected.content)
			return err
		},
	}
}
