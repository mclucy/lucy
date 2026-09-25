package cmd

import "github.com/spf13/cobra"

// configCmd is intentionally not registered with rootCmd.
var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage lucy's configurations",
	RunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
}
