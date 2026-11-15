package docs

import (
	"fmt"
	"io"

	docscontent "github.com/mclucy/lucy/docs"
	"github.com/mclucy/lucy/internal/cli"
	"github.com/mclucy/lucy/internal/cli/help"
	"github.com/mclucy/lucy/terminal/style"
	"github.com/spf13/cobra"
)

func newListCommand(inv *cli.Invocation) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: help.DocsListShort,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			pages, err := docscontent.Load()
			if err != nil {
				return err
			}
			return cli.PageDocument(cmd, inv, cmd.OutOrStdout(), func(out io.Writer, _ style.Terminal) error {
				for _, page := range pages {
					if _, err := fmt.Fprintln(out, page.Slug); err != nil {
						return err
					}
				}
				return nil
			})
		},
	}
}
