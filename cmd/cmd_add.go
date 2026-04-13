package cmd

import (
	"context"

	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/syntax"
	"github.com/mclucy/lucy/tools"
	"github.com/mclucy/lucy/types"
	"github.com/urfave/cli/v3"
)

const (
	flagForceName        = "force"
	flagWithOptionalName = "with-optional"
	flagNoOptionalName   = "no-optional"
)

var subcmdAdd = &cli.Command{
	Name:  "add",
	Usage: "Add new mods, plugins, or server modules",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:    flagForceName,
			Aliases: []string{"f"},
			Usage:   "Ignore version, dependency, and platform warnings",
			Value:   false,
		},
		&cli.BoolFlag{
			Name:  flagWithOptionalName,
			Usage: "Also install optional upstream dependencies",
			Value: false,
		},
		&cli.BoolFlag{
			Name:  flagNoOptionalName,
			Usage: "Skip optional upstream dependencies (default)",
			Value: false,
		},
		flagNoStyle,
	},
	ArgsUsage: "<package-identifier> [package-identifier...]",
	Action: tools.Decorate(
		actionAdd,
		decoratorGlobalFlags,
		decoratorHelpAndExitOnNoArg,
		decoratorLogAndExitOnError,
	),
	ShellComplete: func(_ context.Context, cmd *cli.Command) {
		if CompleteFlagNamesIfRequested(cmd) {
			return
		}

		token := ""
		if cmd.NArg() > 0 {
			token = cmd.Args().Get(cmd.NArg() - 1)
		}
		CompletePackageIDSuggestions(context.Background(), cmd, token)
	},
}

var actionAdd cli.ActionFunc = func(
	_ context.Context,
	cmd *cli.Command,
) error {
	withOptional := cmd.Bool(flagWithOptionalName)
	noOptional := cmd.Bool(flagNoOptionalName)
	if withOptional && noOptional {
		return cli.Exit(
			"--with-optional and --no-optional cannot be used together",
			1,
		)
	}

	options := install.DefaultOptions()
	options.WithOptional = withOptional

	args := cmd.Args().Slice()
	ids := make([]types.PackageId, 0, len(args))
	for _, arg := range args {
		ids = append(ids, syntax.Parse(arg))
	}

	if len(ids) > 1 {
		return install.InstallMany(ids, types.SourceAuto, options)
	}

	id := ids[0]
	if id.Version == types.VersionAny {
		// override the default parse for empty version to be the latest
		// compatible version, which is more likely what users want.
		id.Version = types.VersionCompatible
	}
	return install.Install(id, types.SourceAuto, options)
}
