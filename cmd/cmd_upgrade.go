package cmd

import "github.com/spf13/cobra"

var upgradeCmd = &cobra.Command{
	Use:     "upgrade",
	Short:   "Upgrade installed packages",
	PreRunE: nil,
	RunE:    nil,
}

func init() {
	rootCmd.AddCommand(upgradeCmd)
}
