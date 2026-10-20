package cli

import (
	"github.com/mclucy/lucy/internal/cli/help"

	"github.com/spf13/cobra"
)

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: help.ExportShort,
	RunE:  NotImplementedRun,
}

func NewExportCommand() *cobra.Command {
	return exportCmd
}
