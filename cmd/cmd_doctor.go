package cmd

import (
	"github.com/mclucy/lucy/internal/cli"
	"github.com/mclucy/lucy/internal/cli/message"

	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: message.DoctorShort,
	Args:  cobra.NoArgs,
	RunE:  cli.NotImplementedRun,
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}
