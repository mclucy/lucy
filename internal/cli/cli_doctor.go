package cli

import (
	"github.com/mclucy/lucy/internal/cli/help"

	"github.com/spf13/cobra"
)

// Doctor is advertised but not implemented. Its file and Java diagnostics have
// no agreed contract yet; see https://github.com/mclucy/lucy/issues/172.
var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: help.DoctorShort,
	RunE:  NotImplementedRun,
}

func NewDoctorCommand() *cobra.Command {
	return doctorCmd
}
