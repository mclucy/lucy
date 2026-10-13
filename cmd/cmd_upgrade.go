package cmd

import (
	"github.com/mclucy/lucy/internal/cli"
	"github.com/mclucy/lucy/internal/cli/message"

	"github.com/spf13/cobra"
)

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: message.UpgradeShort,
	RunE:  cli.NotImplementedRun,
}

func init() {
	rootCmd.AddCommand(upgradeCmd)
}
