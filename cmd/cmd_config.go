package cmd

import (
	"github.com/mclucy/lucy/internal/cli/message"

	"github.com/spf13/cobra"
)

// configCmd is intentionally not registered with rootCmd.
var configCmd = &cobra.Command{
	Use:   "config",
	Short: message.ConfigShort,
	RunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
}
