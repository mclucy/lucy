package cli

import (
	"github.com/mclucy/lucy/internal/cli/message"

	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: message.DoctorShort,
	Args:  cobra.NoArgs,
	RunE:  NotImplementedRun,
}

func NewDoctorCommand() *cobra.Command {
	return doctorCmd
}
