package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"charm.land/fang/v2"
	"github.com/mclucy/lucy/internal/agent"
	"github.com/mclucy/lucy/internal/cli"
	"github.com/mclucy/lucy/internal/cli/compile"
	docscli "github.com/mclucy/lucy/internal/cli/docs"
	"github.com/mclucy/lucy/internal/cli/file"
	"github.com/mclucy/lucy/internal/cli/info"
	"github.com/mclucy/lucy/internal/cli/search"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/report"
	"github.com/mclucy/lucy/terminal/style"
	"github.com/spf13/cobra"
)

var (
	version           string
	commit            string
	executingCommand  *cobra.Command
	loggingReady      bool
	selectedJSON      cli.JSONRendering
	machineResult     bytes.Buffer
	resultWriter      io.Writer
	flagParsingFailed bool
	agentHelpInjected bool
)

const agentInstructions = "Instructions for agents:\n" +
	"The --help flag is human-oriented. Use `lucy docs list` to view all available documentation,\n" +
	"`lucy docs grep` to search, and `lucy docs view` to read articles. `--json-compact` and\n" +
	"`--no-interactive` will be on by default for you. You can use tools like `jq` to process\n" +
	"command outputs."

var rootCmd = &cobra.Command{
	Use:   "lucy",
	Short: "The Minecraft server package manager",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		executingCommand = cmd
		mode := effectiveRendering(cmd)
		selectedJSON = mode
		if mode == cli.JSONNone {
			report.SetOutput(cmd.ErrOrStderr())
		} else {
			// Machine mode owns the streams: no human notice may join the
			// result or error document.
			report.SetOutput(io.Discard)
		}
		if err := configureLogging(cmd); err != nil {
			return err
		}
		loggingReady = true
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
		if mode != cli.JSONNone {
			// Machine mode has no interactive surface: prompts fail and
			// progress renderers stay off.
			style.TurnOffInteractivity()
		}
		if n := cmd.Name(); n != "__complete" && n != "__completeNoDesc" {
			directory, err := os.Getwd()
			if err != nil {
				log.Debug("action working directory unavailable", "error", err)
			}
			log.Info("action invoked", "action", cmd.CommandPath(), "directory", directory)
			if mode == cli.JSONNone && !docsCommand(cmd) {
				report.Warn(fmt.Errorf(
					"lucy is on pre-alpha, it is not intended to be used in production environments and may break your server",
				))
			}
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
	// stderr carries exactly one document in machine mode, so terminal log
	// mirroring stays off even when the caller passed --print-logs. The log
	// file keeps recording everything.
	if effectiveRendering(cmd) != cli.JSONNone {
		printLogs = false
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
		"Mirror diagnostic logs to stderr; machine output keeps stderr clean",
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
		docscli.NewCommand(),
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

// applyAgentDefaults makes the non-interactive and compact-JSON behaviors
// implicit for detected agents. Explicit flags still win because parsing runs
// after these defaults are installed.
func applyAgentDefaults() {
	setPersistentBoolDefault(cli.FlagNoInteractive, false)
	setPersistentBoolDefault(cli.FlagJSONCompact, false)
	if !agent.Detected() {
		return
	}
	setPersistentBoolDefault(cli.FlagNoInteractive, true)
	if !jsonDefaultSupported(invocationCommand()) {
		return
	}
	if cli.JSONFlagsPresent(os.Args[1:]) {
		return
	}
	setPersistentBoolDefault(cli.FlagJSONCompact, true)
}

func setPersistentBoolDefault(name string, value bool) {
	flag := rootCmd.PersistentFlags().Lookup(name)
	if flag == nil {
		return
	}
	text := "false"
	if value {
		text = "true"
	}
	_ = flag.Value.Set(text)
	flag.DefValue = text
}

func docsCommand(cmd *cobra.Command) bool {
	commandPath := cmd.CommandPath()
	return commandPath == "lucy docs" ||
		strings.HasPrefix(commandPath, "lucy docs ")
}

// jsonDefaultSupported excludes commands that reject or ignore JSON output.
func jsonDefaultSupported(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	switch cmd.Name() {
	case "__complete", "__completeNoDesc", "completion", "help", "man":
		return false
	}
	commandPath := cmd.CommandPath()
	if commandPath == "lucy" || commandPath == "lucy compile" {
		return false
	}
	return !strings.HasPrefix(commandPath, "lucy docs")
}

// injectAgentInstructions adds the agent notice to every help text. Help is
// rendered from Long, so this works for both fang and Cobra help paths.
func injectAgentInstructions() {
	if agentHelpInjected || !agent.Detected() {
		return
	}
	agentHelpInjected = true
	block := "\n\n" + agentInstructions
	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		switch {
		case cmd.Long != "":
			cmd.Long += block
		case cmd.Short != "":
			cmd.Long = cmd.Short + block
		}
		for _, child := range cmd.Commands() {
			walk(child)
		}
	}
	walk(rootCmd)
}

// effectiveRendering reports the rendering requested by the actual
// invocation. Parsed flags win; otherwise the raw command line is inspected,
// so a failure that happens before Cobra parses flags still honors --json and
// --json-compact.
func effectiveRendering(cmd *cobra.Command) cli.JSONRendering {
	if docsCommand(cmd) {
		return cli.JSONNone
	}
	if cmd.Flags().Parsed() && !flagParsingFailed {
		return cli.JSONMode(cmd)
	}
	mode := cli.JSONModeFromArgs(cmd, os.Args[1:])
	if mode == cli.JSONNone && !cli.JSONFlagsPresent(os.Args[1:]) {
		if compact, _ := cmd.Root().PersistentFlags().GetBool(cli.FlagJSONCompact); compact {
			return cli.JSONCompact
		}
	}
	return mode
}

// invocationCommand finds the command the raw command line addresses, even
// when Cobra stopped before setting executingCommand.
func invocationCommand() *cobra.Command {
	if executingCommand != nil {
		return executingCommand
	}
	command, _, _ := rootCmd.Find(os.Args[1:])
	if command != nil {
		return command
	}
	return rootCmd
}

// renderError keeps human failures readable: structured details render their
// own multi-line explanation, ordinary errors render on one line, and usage
// mistakes keep the framework's format with its --help hint. Machine mode
// defers rendering to the CLI boundary so the log file can close first and a
// close failure joins the same document.
func renderError(w io.Writer, styles fang.Styles, err error) {
	if err == nil {
		return
	}
	var exitErr *cli.ExitError
	if errors.As(err, &exitErr) && exitErr.Silent {
		return
	}
	command := invocationCommand()
	if mode := effectiveRendering(command); mode != cli.JSONNone {
		selectedJSON = mode
		return
	}
	var detail *report.Detail
	if errors.As(err, &detail) && detail != nil {
		report.RenderDetail(w, detail)
		fmt.Fprintf(w, "  %s %s\n", style.Muted("log:"), log.FilePath())
		return
	}
	if cli.IsFrameworkUsageError(err) {
		fang.DefaultErrorHandler(w, styles, err)
		return
	}
	report.Error(err)
	fmt.Fprintf(w, "  %s %s\n", style.Muted("log:"), log.FilePath())
}

// WriteError renders a failure through the same mode-aware funnel outside
// command execution. main uses it for a log file that fails to close.
func WriteError(err error) {
	if err == nil {
		return
	}
	command := invocationCommand()
	if mode := effectiveRendering(command); mode != cli.JSONNone {
		selectedJSON = mode
	}
	output := cli.NewOperationOutput(command)
	output.SetJSONRendering(selectedJSON)
	output.WriteError(err, log.FilePath())
}

// JSONSelected reports whether the actual invocation selected machine output.
func JSONSelected() bool {
	return selectedJSON != cli.JSONNone
}

// FlushOutput publishes a machine result only after command cleanup succeeds.
func FlushOutput() error {
	if !JSONSelected() || resultWriter == nil {
		return nil
	}
	if _, err := machineResult.WriteTo(resultWriter); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	return nil
}

func Execute() error {
	displayVersion, displayCommit := buildVersion()
	executingCommand = nil
	loggingReady = false
	flagParsingFailed = false
	applyAgentDefaults()
	injectAgentInstructions()
	command := invocationCommand()
	if docsCommand(command) {
		selectedJSON = cli.JSONNone
	} else {
		selectedJSON = cli.JSONModeFromArgs(command, os.Args[1:])
		if selectedJSON == cli.JSONNone && !cli.JSONFlagsPresent(os.Args[1:]) {
			if compact, _ := rootCmd.PersistentFlags().GetBool(cli.FlagJSONCompact); compact {
				selectedJSON = cli.JSONCompact
			}
		}
	}
	rootCmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		flagParsingFailed = true
		return err
	})
	machineResult.Reset()
	resultWriter = nil
	if JSONSelected() {
		resultWriter = rootCmd.OutOrStdout()
		rootCmd.SetOut(&machineResult)
		defer rootCmd.SetOut(resultWriter)
	}
	var err error
	if JSONSelected() {
		// Fang probes terminal colors before invoking its error handler.
		// Machine mode must not emit those queries on the real stdout.
		rootCmd.SilenceErrors = true
		rootCmd.SilenceUsage = true
		rootCmd.Version = displayVersion
		_, err = rootCmd.ExecuteContextC(context.Background())
	} else {
		err = fang.Execute(
			context.Background(),
			rootCmd,
			fang.WithVersion(displayVersion),
			fang.WithCommit(displayCommit),
			fang.WithErrorHandler(renderError),
		)
	}
	if err != nil {
		command := executingCommand
		if command == nil {
			var findErr error
			command, _, findErr = rootCmd.Find(os.Args[1:])
			if command == nil {
				command = rootCmd
			}
			if findErr != nil {
				// Unknown commands fail before Cobra parses the persistent
				// flags. Keep the flag failure for the log without rendering a
				// second human error.
				if flagErr := rootCmd.ParseFlags(os.Args[1:]); flagErr != nil {
					err = errors.Join(err, flagErr)
				}
			}
			if configureErr := configureLogging(command); configureErr != nil {
				if !JSONSelected() {
					report.Error(configureErr)
				}
				return errors.Join(err, configureErr)
			}
			loggingReady = true
		}
		if loggingReady {
			var exitErr *cli.ExitError
			if !errors.As(err, &exitErr) || !exitErr.Silent {
				log.Error(fmt.Errorf("%s failed: %w", command.CommandPath(), err))
			}
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
