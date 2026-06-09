package cmd

import (
	"fmt"

	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/tools"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:           "lucy",
	Short:         "The Minecraft server package manager",
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if noStyle, _ := cmd.Flags().GetBool(flagNoStyleName); noStyle {
			tools.TurnOffStyles()
		}
		if logFile, _ := cmd.Flags().GetBool(flagLogFileName); logFile {
			fmt.Println("Log file at", logger.GetLogFile().Name())
		}
		if printLogs, _ := cmd.Flags().GetBool(flagPrintLogsName); printLogs {
			logger.EnablePrintLogs()
		}
		if debug, _ := cmd.Flags().GetBool(flagDebugName); debug {
			logger.EnableDebug()
		}
		if dumpLogs, _ := cmd.Flags().GetBool(flagDumpLogsName); dumpLogs {
			logger.EnableDumpHistory()
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

	rootCmd.SetFlagErrorFunc(
		func(cmd *cobra.Command, err error) error {
			fmt.Fprintln(cmd.ErrOrStderr(), err)
			cmd.Usage()
			return err
		},
	)
}

// runWithErrorLogging wraps a RunE function to log errors via logger.ReportError.
// It replaces the decoratorLogAndExitOnError decorator.
func runWithErrorLogging(
	fn func(
		cmd *cobra.Command,
		args []string,
	) error,
) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		err := fn(cmd, args)
		if err != nil {
			logger.ReportError(err)
		}
		return err
	}
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}
