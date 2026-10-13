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
		if n := cmd.Name(); n != "__complete" && n != "__completeNoDesc" {
			log.ReportWarn(
				fmt.Errorf(
					"lucy is on pre-alpha, it is not intended to be used in production environments and may break your server",
				),
			)
		}
		if noStyle, _ := cmd.Flags().GetBool(cli.FlagNoStyle); noStyle {
			style.TurnOffStyles()
			log.TurnOffColors()
		}
		if noColor, _ := cmd.Flags().GetBool(cli.FlagNoColor); noColor {
			style.TurnOffColors()
			log.TurnOffColors()
		}
		if noInteractive, _ := cmd.Flags().GetBool(cli.FlagNoInteractive); noInteractive {
			style.TurnOffInteractivity()
		}
		if logFile, _ := cmd.Flags().GetBool(cli.FlagLogFile); logFile {
			fmt.Println("Log file at", log.GetLogFile().Name())
		}
		if printLogs, _ := cmd.Flags().GetBool(cli.FlagPrintLogs); printLogs {
			log.EnablePrintLogs()
		}
		if debug, _ := cmd.Flags().GetBool(cli.FlagDebug); debug {
			log.EnableDebug()
		}
		if dumpLogs, _ := cmd.Flags().GetBool(cli.FlagDumpLogs); dumpLogs {
			log.EnableDumpHistory()
		}
		return nil
	},
}

func init() {
	rootCmd.PersistentFlags().Bool(cli.FlagDebug, false, "Show debug logs")
	rootCmd.PersistentFlags().Bool(
		cli.FlagLogFile,
		false,
		"Output the path to logfile",
	)
	rootCmd.PersistentFlags().Bool(
		cli.FlagPrintLogs,
		false,
		"Print logs to console",
	)
	rootCmd.PersistentFlags().Bool(
		cli.FlagDumpLogs,
		false,
		"Dump the log history to console before exit",
	)
	_ = rootCmd.PersistentFlags().MarkHidden(cli.FlagDumpLogs)
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
