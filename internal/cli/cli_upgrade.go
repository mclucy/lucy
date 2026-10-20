package cli

import (
	"github.com/mclucy/lucy/internal/cli/help"

	"github.com/spf13/cobra"
)

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: help.UpgradeShort,
	RunE:  NotImplementedRun,
}

func NewUpgradeCommand() *cobra.Command {
	return upgradeCmd
}
