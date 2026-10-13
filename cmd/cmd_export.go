package cmd

import (
	"github.com/mclucy/lucy/internal/cli"

	"github.com/spf13/cobra"
)

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export server configuration, or generate client",
	RunE:  cli.NotImplementedRun,
}

func init() {
	rootCmd.AddCommand(exportCmd)
}
