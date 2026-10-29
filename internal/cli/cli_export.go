package cli

import (
	"github.com/mclucy/lucy/internal/cli/help"

	"github.com/spf13/cobra"
)

func NewExportCommand(_ *Invocation) *cobra.Command {
	return &cobra.Command{
		Use:   "export",
		Short: help.ExportShort,
		RunE:  NotImplementedRun,
	}
}
