package cmd

import (
	"context"
	"fmt"

	"charm.land/fang/v2"
	"github.com/mclucy/lucy/internal/cli"
	"github.com/mclucy/lucy/internal/cli/add"
	"github.com/mclucy/lucy/internal/cli/bisect"
	"github.com/mclucy/lucy/internal/cli/create"
	"github.com/mclucy/lucy/internal/cli/info"
	lucyinit "github.com/mclucy/lucy/internal/cli/init"
	"github.com/mclucy/lucy/internal/cli/install"
	"github.com/mclucy/lucy/internal/cli/search"
	"github.com/mclucy/lucy/internal/cli/status"
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
					"lucy is on pre-alpha, it is not intended to be used in production environments and may break your server.",
				),
			)
		}
		if noStyle, _ := cmd.Flags().GetBool(cli.FlagNoStyle); noStyle {
			style.TurnOffStyles()
			log.TurnOffStyles()
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
		cli.FlagNoStyle,
		false,
		"Disable colored and styled output",
	)
	rootCmd.PersistentFlags().Bool(
		cli.FlagJSONCompact,
		false,
		"Print raw JSON response without indentation",
	)
	rootCmd.PersistentFlags().String(
		cli.FlagServer,
		"",
		"Operate on a registered Lucy server instance",
	)

	rootCmd.AddCommand(
		add.NewCommand(),
		bisect.NewCommand(),
		info.NewCommand(),
		lucyinit.NewCommand(),
		install.NewCommand(),
		create.NewCommand(),
		search.NewCommand(),
		status.NewCommand(),
	)
}

// Execute runs the root command.
func Execute() error {
	return fang.Execute(
		context.Background(),
		rootCmd,
		fang.WithVersion(version),
		fang.WithCommit(commit),
	)
}
