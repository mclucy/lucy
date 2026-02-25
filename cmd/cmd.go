package cmd

import (
	"context"
	"fmt"

	"github.com/mclucy/lucy/tools"

	"github.com/urfave/cli/v3"
)

// Frontend should change when user do not run the program in CLI
// This is prepared for possible GUI implementation
var Frontend = "cli"

// Each subcommand (and its action function) should be in its own file

// Cli is the main command for lucy
var Cli = &cli.Command{
	Name:  "lucy",
	Usage: "The Minecraft server-side package manager",
	Action: tools.Decorate(
		actionEmpty,
		decoratorBaseCommandFlags,
		decoratorGlobalFlags,
		decoratorHelpAndExitOnNoArg,
		decoratorHelpAndExitOnError,
	),
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:    "log-file",
			Aliases: []string{"l"},
			Usage:   "Output the path to logfile",
			Value:   false,
		},
		&cli.BoolFlag{
			Name:  "print-logs",
			Usage: "Print logs to console",
			Value: false,
		},
		&cli.BoolFlag{
			Name:  "debug",
			Usage: "Show debug logs",
			Value: false,
		},
		&cli.BoolFlag{
			Name:   "dump-logs",
			Usage:  "Dump the log history to console before exit",
			Value:  false,
			Hidden: true,
		},
		flagNoStyle,
	},
	Commands: []*cli.Command{
		subcmdStatus,
		subcmdInfo,
		subcmdSearch,
		subcmdAdd,
		subcmdInit,
	},
	EnableShellCompletion:  true,
	Suggest:                true,
	UseShortOptionHandling: true,
	DefaultCommand:         "help",
	OnUsageError:           helpOnUsageError,
}

var helpOnUsageError cli.OnUsageErrorFunc = func(
	ctx context.Context,
	cmd *cli.Command,
	err error,
	isSubcommand bool,
) error {
	if isSubcommand {
		fmt.Println(fmt.Errorf("invalid command: %s", err).Error())
		cli.ShowAppHelpAndExit(cmd, 1)
	}
	fmt.Println(err.Error())
	return err
}

var actionEmpty cli.ActionFunc = func(
	ctx context.Context,
	cmd *cli.Command,
) error {
	return nil
}
