package cli

import "github.com/spf13/cobra"

// RegisterPackageTaskFlags copies the public commands' option definitions so
// hidden subprocesses parse exactly the same options, including future additions.
// The daemon serializer still validates which options belong to each task.
func RegisterPackageTaskFlags(cmd *cobra.Command, contracts *JSONContracts) {
	prototypes := []*cobra.Command{
		NewAddCommand(NewJSONContracts(), nil, nil),
		NewInstallCommand(NewJSONContracts(), nil, nil),
		NewRemoveCommand(NewJSONContracts(), nil, nil),
	}
	for _, prototype := range prototypes {
		cmd.Flags().AddFlagSet(prototype.Flags())
	}
	installJSON.Bind(contracts, cmd)
}
