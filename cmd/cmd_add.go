package cmd

import (
	"context"
	"fmt"

	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/syntax"
	"github.com/mclucy/lucy/types"
	"github.com/spf13/cobra"
)

const (
	flagForceName        = "force"
	flagWithOptionalName = "with-optional"
	flagNoOptionalName   = "no-optional"
)

var addCmd = &cobra.Command{
	Use:   "add",
	Short: "Add new mods, plugins, or server modules",
	Args:  cobra.MinimumNArgs(1),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return CompletePackageIDSuggestions(context.Background(), "add", toComplete)
	},
	PreRunE: func(cmd *cobra.Command, args []string) error {
		withOptional, _ := cmd.Flags().GetBool(flagWithOptionalName)
		noOptional, _ := cmd.Flags().GetBool(flagNoOptionalName)
		if withOptional && noOptional {
			return fmt.Errorf("--with-optional and --no-optional cannot be used together")
		}
		return nil
	},
	RunE: runWithErrorLogging(actionAdd),
}

func init() {
	addCmd.Flags().BoolP(flagForceName, "f", false, "Ignore version, dependency, and platform warnings")
	addCmd.Flags().Bool(flagWithOptionalName, false, "Also install optional upstream dependencies")
	addCmd.Flags().Bool(flagNoOptionalName, false, "Skip optional upstream dependencies (default)")
	addNoStyleFlag(addCmd)
	rootCmd.AddCommand(addCmd)
}

func actionAdd(cmd *cobra.Command, args []string) error {
	withOptional, _ := cmd.Flags().GetBool(flagWithOptionalName)

	options := install.DefaultOptions()
	options.WithOptional = withOptional

	ids := make([]types.PackageId, 0, len(args))
	for _, arg := range args {
		id, err := syntax.Parse(arg)
		if err != nil {
			logger.Fatal(err)
		}
		ids = append(ids, id)
	}

	if len(ids) > 1 {
		return install.InstallMany(ids, types.SourceAuto, options)
	}

	id := ids[0]
	if id.Version == types.VersionAny {
		id.Version = types.VersionCompatible
	}
	return install.Install(id, types.SourceAuto, options)
}
