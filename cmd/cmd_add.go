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
	flagForceName = "force"
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
		flagNoStyle,
	},
	ArgsUsage: "<package-identifier>",
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
			token = cmd.Args().First()
		}
		CompletePackageIDSuggestions(context.Background(), cmd, token)
	},
}

var actionAdd cli.ActionFunc = func(
	_ context.Context,
	cmd *cli.Command,
) error {
	id := syntax.Parse(cmd.Args().First())
	if id.Version == types.VersionAny {
		// override the default parse for empty version to be the latest
		// compatible version, which is more likely what users want.
		id.Version = types.VersionCompatible
	}
	return install.Install(id, types.SourceAuto)
}
