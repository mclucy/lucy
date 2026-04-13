package cmd

import "github.com/spf13/cobra"

// configCmd is defined but intentionally not registered with rootCmd.
// It is a stub that preserves the current command surface of exactly 6 top-level subcommands.
var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage lucy's configurations",
	RunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
}
