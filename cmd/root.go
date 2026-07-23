package cmd

import (
	"context"
	"fmt"

	"charm.land/fang/v2"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/tui/style"
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
		if noStyle, _ := cmd.Flags().GetBool(flagNoStyleName); noStyle {
			style.TurnOffStyles()
			log.TurnOffStyles()
		}
		if logFile, _ := cmd.Flags().GetBool(flagLogFileName); logFile {
			fmt.Println("Log file at", log.GetLogFile().Name())
		}
		if printLogs, _ := cmd.Flags().GetBool(flagPrintLogsName); printLogs {
			log.EnablePrintLogs()
		}
		if debug, _ := cmd.Flags().GetBool(flagDebugName); debug {
			log.EnableDebug()
		}
		if dumpLogs, _ := cmd.Flags().GetBool(flagDumpLogsName); dumpLogs {
			log.EnableDumpHistory()
		}
		return nil
	},
}

func init() {
	rootCmd.PersistentFlags().Bool(flagDebugName, false, "Show debug logs")
	rootCmd.PersistentFlags().Bool(
		flagLogFileName,
		false,
		"Output the path to logfile",
	)
	rootCmd.PersistentFlags().Bool(
		flagPrintLogsName,
		false,
		"Print logs to console",
	)
	rootCmd.PersistentFlags().Bool(
		flagDumpLogsName,
		false,
		"Dump the log history to console before exit",
	)
	_ = rootCmd.PersistentFlags().MarkHidden(flagDumpLogsName)
	rootCmd.PersistentFlags().Bool(
		flagNoStyleName,
		false,
		"Disable colored and styled output",
	)
	rootCmd.PersistentFlags().Bool(
		flagJsonCompactName,
		false,
		"Print raw JSON response without indentation",
	)
}

// runWithErrorLogging records command failures in the log file only.
// User-facing errors are returned to fang/cobra for a single stderr presentation.
func runWithErrorLogging(
	fn func(
		cmd *cobra.Command,
		args []string,
	) error,
) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		err := fn(cmd, args)
		if err != nil {
			log.Error(err)
		}
		return err
	}
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
