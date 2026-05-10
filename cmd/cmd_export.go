package cmd

import "github.com/spf13/cobra"

var exportCmd = &cobra.Command{
	Use:     "export",
	Short:   "Export server configuration, or generate client",
	PreRunE: nil,
	RunE:    nil,
}

func init() {
	rootCmd.AddCommand(exportCmd)
}
