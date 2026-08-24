package cli

import (
	"github.com/mclucy/lucy/log"
	"github.com/spf13/cobra"
)

// WithErrorLogging records command failures in the log file only.
// User-facing errors are returned to fang/cobra for a single stderr presentation.
func WithErrorLogging(
	fn func(
		cmd *cobra.Command,
		args []string,
	) error,
) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		err := fn(cmd, args)
		if err != nil {
			log.Error(err)
		}
		return err
	}
}
