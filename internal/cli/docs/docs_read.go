package docs

import (
	"fmt"

	"github.com/mclucy/lucy/internal/cli/help"
	"github.com/spf13/cobra"
)

func newReadCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "read <page> [page...]",
		Short: help.DocsReadShort,
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pages, err := loadPages()
			if err != nil {
				return err
			}
			// Resolve every argument before writing, so a mistyped page name
			// never leaves half the request on stdout.
			selected := make([]page, 0, len(args))
			for _, arg := range args {
				slug, err := normalizeSlug(arg)
				if err != nil {
					return err
				}
				found, ok := findPage(pages, slug)
				if !ok {
					return fmt.Errorf(
						"unknown documentation page %q; run lucy docs list",
						arg,
					)
				}
				selected = append(selected, found)
			}
			out := cmd.OutOrStdout()
			for _, current := range selected {
				if len(selected) > 1 {
					if _, err := fmt.Fprintf(out, "===== %s =====\n", current.slug); err != nil {
						return err
					}
				}
				if _, err := out.Write(current.content); err != nil {
					return err
				}
				if len(current.content) > 0 && current.content[len(current.content)-1] != '\n' {
					if _, err := fmt.Fprintln(out); err != nil {
						return err
					}
				}
				if len(selected) > 1 {
					if _, err := fmt.Fprintln(out); err != nil {
						return err
					}
				}
			}
			return nil
		},
	}
}
