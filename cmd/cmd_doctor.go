package cmd

import (
	"github.com/mclucy/lucy/internal/cli"

	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnose the server environment",
	Args:  cobra.NoArgs,
	RunE:  cli.NotImplementedRun,
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}
