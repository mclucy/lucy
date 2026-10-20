package cli

import (
	"github.com/mclucy/lucy/internal/cli/help"

	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: help.DoctorShort,
	Args:  cobra.NoArgs,
	RunE:  NotImplementedRun,
}

func NewDoctorCommand() *cobra.Command {
	return doctorCmd
}
