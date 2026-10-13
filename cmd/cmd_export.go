package cmd

import (
	"github.com/mclucy/lucy/internal/cli"
	"github.com/mclucy/lucy/internal/cli/message"

	"github.com/spf13/cobra"
)

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: message.ExportShort,
	RunE:  cli.NotImplementedRun,
}

func init() {
	rootCmd.AddCommand(exportCmd)
}
