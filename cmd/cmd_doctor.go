package cmd

import "github.com/spf13/cobra"

var doctorCmd = &cobra.Command{
	Use:     "doctor",
	Short:   "Diagnose risks in the server environment",
	Args:    cobra.NoArgs,
	PreRunE: nil,
	RunE:    nil,
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}
