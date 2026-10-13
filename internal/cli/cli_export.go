package cli

import (
	"github.com/mclucy/lucy/internal/cli/message"

	"github.com/spf13/cobra"
)

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: message.ExportShort,
	RunE:  NotImplementedRun,
}

func NewExportCommand() *cobra.Command {
	return exportCmd
}
