package cli

import (
	"fmt"
	"os"

	"github.com/mclucy/lucy/internal/cli/help"
	"github.com/mclucy/lucy/operation"
	"github.com/spf13/cobra"
)

var deinitCmd = &cobra.Command{
	Use:   "deinit",
	Short: help.DeinitShort,
	Long:  help.DeinitLong,
	Args:  cobra.NoArgs,
	RunE:  actionDeinit,
}

func NewDeinitCommand() *cobra.Command {
	return deinitCmd
}

// deinitDocument is the machine-readable outcome of one deinitialization.
type deinitDocument struct {
	Removed []string `json:"removed"`
}

func actionDeinit(cmd *cobra.Command, _ []string) error {
	output := NewOperationOutput(cmd)
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("could not determine working directory: %w", err)
	}
	result, err := operation.Deinitialize(workDir)
	if err != nil {
		return err
	}
	document := deinitDocument{Removed: result.Removed}
	if output.JSON() {
		return output.WriteJSON(document)
	}
	out := output.Human()
	for _, name := range result.Removed {
		if _, err := fmt.Fprintf(out, "removed %s\n", name); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintln(out, "Lucy no longer manages this server")
	return err
}
