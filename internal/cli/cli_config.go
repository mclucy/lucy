package cli

import (
	"github.com/mclucy/lucy/internal/cli/help"

	"github.com/spf13/cobra"
)

// configCmd is intentionally not registered with the root command.
var configCmd = &cobra.Command{
	Use:   "config",
	Short: help.ConfigShort,
}

func NewConfigCommand() *cobra.Command {
	return configCmd
}
