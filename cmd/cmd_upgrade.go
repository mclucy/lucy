package cmd

import (
	"github.com/mclucy/lucy/internal/cli"

	"github.com/spf13/cobra"
)

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade installed packages",
	RunE:  cli.NotImplementedRun,
}

func init() {
	rootCmd.AddCommand(upgradeCmd)
}
