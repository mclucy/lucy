package cli

import (
	"github.com/mclucy/lucy/internal/cli/message"

	"github.com/spf13/cobra"
)

// configCmd is intentionally not registered with the root command.
var configCmd = &cobra.Command{
	Use:   "config",
	Short: message.ConfigShort,
}

func NewConfigCommand() *cobra.Command {
	return configCmd
}
