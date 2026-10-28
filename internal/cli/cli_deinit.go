package cli

import (
	"fmt"

	"github.com/mclucy/lucy/internal/cli/help"
	"github.com/spf13/cobra"
)

// deinitDocument is the machine-readable outcome of one deinitialization.
type deinitDocument struct {
	Removed  []string `json:"removed"`
	Warnings []string `json:"warnings,omitempty"`
}

var deinitJSON JSONResult[deinitDocument]

func NewDeinitCommand(contracts *JSONContracts, inv *Invocation, deps *Dependencies) *cobra.Command {
	deinitCmd := &cobra.Command{
		Use:   "deinit",
		Short: help.DeinitShort,
		Long:  help.DeinitLong,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return actionDeinit(cmd, inv, deps)
		},
	}
	deinitJSON.Bind(contracts, deinitCmd)
	return deinitCmd
}

func actionDeinit(cmd *cobra.Command, inv *Invocation, deps *Dependencies) error {
	output := NewOperationOutput(cmd, inv)
	result, err := deps.Operations.Deinitialize(inv.Directory)
	if err != nil {
		return err
	}
	for _, warning := range result.Warnings {
		output.Warn(warning)
	}
	document := deinitDocument{Removed: result.Removed, Warnings: output.Warnings()}
	if output.JSON() {
		return deinitJSON.Write(output, document)
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
