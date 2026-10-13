package cmd

import (
	"context"
	"fmt"

	"charm.land/fang/v2"
	"github.com/mclucy/lucy/internal/cli"
	"github.com/mclucy/lucy/internal/cli/compile"
	"github.com/mclucy/lucy/internal/cli/info"
	"github.com/mclucy/lucy/internal/cli/search"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/report"
	"github.com/mclucy/lucy/terminal/style"
	"github.com/spf13/cobra"
)

var (
	version string
	commit  string
)

var rootCmd = &cobra.Command{
	Use:   "lucy",
	Short: "The Minecraft server package manager",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		path, err := cmd.Flags().GetString(cli.FlagLogFile)
		if err != nil {
			return err
		}
		level, err := cmd.Flags().GetString(cli.FlagLogLevel)
		if err != nil {
			return err
		}
		printLogs, err := cmd.Flags().GetBool(cli.FlagPrintLogs)
		if err != nil {
			return err
		}
		if err := log.Configure(path, level, printLogs); err != nil {
			return err
		}
		report.SetOutput(cmd.ErrOrStderr())
		if noStyle, _ := cmd.Flags().GetBool(cli.FlagNoStyle); noStyle {
			style.TurnOffStyles()
			report.TurnOffColors()
		}
		if noColor, _ := cmd.Flags().GetBool(cli.FlagNoColor); noColor {
			style.TurnOffColors()
			report.TurnOffColors()
		}
		if noInteractive, _ := cmd.Flags().GetBool(cli.FlagNoInteractive); noInteractive {
			style.TurnOffInteractivity()
		}
		if n := cmd.Name(); n != "__complete" && n != "__completeNoDesc" {
			report.Warn(fmt.Errorf(
				"lucy is on pre-alpha, it is not intended to be used in production environments and may break your server",
			))
		}
		return nil
	},
}

func init() {
	rootCmd.PersistentFlags().String(
		cli.FlagLogLevel,
		"info",
		"Set diagnostic log level: debug, info, warn, error, fatal",
	)
	rootCmd.PersistentFlags().String(
		cli.FlagLogFile,
		log.DefaultFile(),
		"Write diagnostic logs to this file",
	)
	rootCmd.PersistentFlags().Bool(
		cli.FlagPrintLogs,
		false,
		"Mirror diagnostic logs to stderr",
	)
	rootCmd.PersistentFlags().Bool(
		cli.FlagJSON,
		false,
		"Print JSON indented with 2 spaces",
	)
	rootCmd.PersistentFlags().Bool(
		cli.FlagJSONCompact,
		false,
		"Print JSON without indentation; wins over --json",
	)
	rootCmd.PersistentFlags().Bool(
		cli.FlagNoColor,
		false,
		"Disable colored output",
	)
	rootCmd.PersistentFlags().Bool(
		cli.FlagNoInteractive,
		false,
		"Disable prompts and animated output; fail instead of asking",
	)
	rootCmd.PersistentFlags().Bool(
		cli.FlagNoStyle,
		false,
		"Same as --no-color --no-interactive",
	)
	rootCmd.PersistentFlags().Bool(
		cli.FlagUseGitHubMirror,
		false,
		"Use the GitHub release mirror for eligible downloads",
	)

	rootCmd.AddCommand(
		cli.NewAddCommand(),
		cli.NewBisectCommand(),
		cli.NewCacheCommand(),
		cli.NewCreateCommand(),
		cli.NewDoctorCommand(),
		cli.NewExportCommand(),
		cli.NewInitCommand(),
		cli.NewInstallCommand(),
		cli.NewLeavesCommand(),
		cli.NewRemoveCommand(),
		cli.NewTreeCommand(),
		cli.NewUpgradeCommand(),
		compile.NewCommand(),
		info.NewCommand(),
		cli.NewStatusCommand(),
		search.NewCommand(),
	)
}

func Execute() error {
	return fang.Execute(
		context.Background(),
		rootCmd,
		fang.WithVersion(version),
		fang.WithCommit(commit),
	)
}
