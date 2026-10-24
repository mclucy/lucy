package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"charm.land/fang/v2"
	"github.com/mclucy/lucy/internal/cli"
	"github.com/mclucy/lucy/internal/cli/compile"
	"github.com/mclucy/lucy/internal/cli/file"
	"github.com/mclucy/lucy/internal/cli/info"
	"github.com/mclucy/lucy/internal/cli/search"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/report"
	"github.com/mclucy/lucy/terminal/style"
	"github.com/spf13/cobra"
)

var (
	version          string
	commit           string
	executingCommand *cobra.Command
	loggingReady     bool
)

var rootCmd = &cobra.Command{
	Use:   "lucy",
	Short: "The Minecraft server package manager",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		executingCommand = cmd
		if err := configureLogging(cmd); err != nil {
			return err
		}
		loggingReady = true
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
			directory, err := os.Getwd()
			if err != nil {
				log.Debug("action working directory unavailable", "error", err)
			}
			log.Info("action invoked", "action", cmd.CommandPath(), "directory", directory)
			report.Warn(fmt.Errorf(
				"lucy is on pre-alpha, it is not intended to be used in production environments and may break your server",
			))
		}
		return nil
	},
}

func configureLogging(cmd *cobra.Command) error {
	flags := cmd.Root().PersistentFlags()
	path, err := flags.GetString(cli.FlagLogFile)
	if err != nil {
		return err
	}
	level, err := flags.GetString(cli.FlagLogLevel)
	if err != nil {
		return err
	}
	printLogs, err := flags.GetBool(cli.FlagPrintLogs)
	if err != nil {
		return err
	}
	return log.Configure(path, level, printLogs)
}

func init() {
	rootCmd.PersistentFlags().String(
		cli.FlagLogLevel,
		"info",
		"Level mirrored by --print-logs; the log file always records debug",
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
		cli.NewDeinitCommand(),
		cli.NewDoctorCommand(),
		file.NewCommand(),
		cli.NewExportCommand(),
		cli.NewInitCommand(),
		cli.NewInstallCommand(),
		cli.NewJavaCommand(),
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

// renderError keeps failures readable: structured details render their own
// multi-line explanation, ordinary errors render on one line, and usage
// mistakes keep the framework's format with its --help hint.
func renderError(w io.Writer, styles fang.Styles, err error) {
	var detail *report.Detail
	if errors.As(err, &detail) {
		report.RenderDetail(w, detail)
		fmt.Fprintf(w, "  %s %s\n", style.Muted("log:"), log.FilePath())
		return
	}
	if isUsageError(err) {
		fang.DefaultErrorHandler(w, styles, err)
		return
	}
	report.Error(err)
	fmt.Fprintf(w, "  %s %s\n", style.Muted("log:"), log.FilePath())
}

// isUsageError keeps flag-parsing mistakes on the framework's surface, where
// they come with the --help hint.
func isUsageError(err error) bool {
	message := err.Error()
	for _, prefix := range []string{
		"flag needs an argument:",
		"unknown flag:",
		"unknown shorthand flag:",
		"unknown command",
		"invalid argument",
	} {
		if strings.HasPrefix(message, prefix) {
			return true
		}
	}
	return false
}

func Execute() error {
	executingCommand = nil
	loggingReady = false
	err := fang.Execute(
		context.Background(),
		rootCmd,
		fang.WithVersion(version),
		fang.WithCommit(commit),
		fang.WithErrorHandler(renderError),
	)
	if err != nil {
		command := executingCommand
		if command == nil {
			var findErr error
			command, _, findErr = rootCmd.Find(os.Args[1:])
			if findErr != nil {
				command = rootCmd
				// Unknown commands fail before Cobra parses the persistent flags.
				if flagErr := rootCmd.ParseFlags(os.Args[1:]); flagErr != nil {
					report.Error(flagErr)
					err = errors.Join(err, flagErr)
				}
			}
			if configureErr := configureLogging(command); configureErr != nil {
				report.Error(configureErr)
				return errors.Join(err, configureErr)
			}
			loggingReady = true
		}
		if loggingReady {
			log.Error(fmt.Errorf("%s failed: %w", command.CommandPath(), err))
		}
		return err
	}
	if loggingReady && executingCommand != nil {
		if n := executingCommand.Name(); n != "__complete" && n != "__completeNoDesc" {
			log.Info("action completed", "action", executingCommand.CommandPath())
		}
	}
	return nil
}
