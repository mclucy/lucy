package cli

import (
	"github.com/mclucy/lucy/internal/cli/message"

	"github.com/spf13/cobra"
)

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: message.UpgradeShort,
	RunE:  NotImplementedRun,
}

func NewUpgradeCommand() *cobra.Command {
	return upgradeCmd
}
