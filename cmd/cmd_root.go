package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"

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
	version string
	commit  string
)

const agentInstructions = "Instructions for agents:\n" +
	"The --help flag is human-oriented. Use `lucy docs list` to view all available documentation,\n" +
	"`lucy docs grep` to search, and `lucy docs read` to read one or more articles. `--json-compact` and\n" +
	"`--no-interactive` will be on by default for you. You can use tools like `jq` to process\n" +
	"command outputs. Use `lucy schema` to list JSON result contracts, `lucy schema COMMAND` to\n" +
	"read a result schema, and `lucy schema --error` to read the shared failure schema."

// Runner owns one invocation: its command tree, JSON contracts, streams, and
// output state. Construct a runner per run; nothing is shared between runs.
type Runner struct {
	root              *cobra.Command
	contracts         *cli.JSONContracts
	args              []string
	stdin             io.Reader
	stdout            io.Writer
	stderr            io.Writer
	executing         *cobra.Command
	loggingReady      bool
	selectedJSON      cli.JSONRendering
	machineResult     bytes.Buffer
	resultWriter      io.Writer
	flagParsingFailed bool
	agentHelpInjected bool
}

// NewRunner prepares an isolated invocation with a fresh command tree, fresh
// JSON contracts, and the given streams.
func NewRunner(args []string, stdin io.Reader, stdout, stderr io.Writer) *Runner {
	r := &Runner{
		contracts: cli.NewJSONContracts(),
		args:      args,
		stdin:     stdin,
		stdout:    stdout,
		stderr:    stderr,
	}
	r.root = NewRootCommand(r.contracts)
	r.root.SetArgs(r.args)
	r.root.SetIn(r.stdin)
	r.root.SetOut(r.stdout)
	r.root.SetErr(r.stderr)
	r.root.PersistentPreRunE = r.persistentPreRunE
	return r
}

// NewRootCommand builds a fresh root with its persistent flags and
// subcommands bound to contracts. Agent defaults are applied per run, not
// here.
func NewRootCommand(contracts *cli.JSONContracts) *cobra.Command {
	root := &cobra.Command{
		Use:   "lucy",
		Short: "The Minecraft server package manager",
	}
	root.PersistentFlags().String(
		cli.FlagLogLevel,
		"info",
		"Level mirrored by --print-logs; the log file always records debug",
	)
	root.PersistentFlags().String(
		cli.FlagLogFile,
		log.DefaultFile(),
		"Write diagnostic logs to this file",
	)
	root.PersistentFlags().Bool(
		cli.FlagPrintLogs,
		false,
		"Mirror diagnostic logs to stderr; machine output keeps stderr clean",
	)
	root.PersistentFlags().Bool(
		cli.FlagJSON,
		false,
		"Print JSON indented with 2 spaces",
	)
	root.PersistentFlags().Bool(
		cli.FlagJSONCompact,
		false,
		"Print JSON without indentation; wins over --json",
	)
	root.PersistentFlags().Bool(
		cli.FlagNoColor,
		false,
		"Disable colored output",
	)
	root.PersistentFlags().Bool(
		cli.FlagNoInteractive,
		false,
		"Disable prompts and animated output; fail instead of asking",
	)
	root.PersistentFlags().Bool(
		cli.FlagNoStyle,
		false,
		"Same as --no-color --no-interactive",
	)
	root.PersistentFlags().Bool(
		cli.FlagUseGitHubMirror,
		false,
		"Use the GitHub release mirror for eligible downloads",
	)

	docs := docscli.NewCommand()
	contracts.Ignore(docs)
	root.AddCommand(
		cli.NewAddCommand(contracts),
		cli.NewBisectCommand(contracts),
		cli.NewCacheCommand(contracts),
		cli.NewConfigCommand(contracts),
		cli.NewCreateCommand(contracts),
		cli.NewDeinitCommand(contracts),
		docs,
		cli.NewDoctorCommand(),
		file.NewCommand(contracts),
		cli.NewExportCommand(),
		cli.NewInitCommand(contracts),
		cli.NewInstallCommand(contracts),
		cli.NewJavaCommand(contracts),
		cli.NewLeavesCommand(contracts),
		cli.NewRemoveCommand(contracts),
		cli.NewTreeCommand(contracts),
		cli.NewUpgradeCommand(contracts),
		compile.NewCommand(),
		info.NewCommand(contracts),
		cli.NewStatusCommand(contracts),
		search.NewCommand(contracts),
		cli.NewSchemaCommand(contracts),
	)
	return root
}

func (r *Runner) persistentPreRunE(cmd *cobra.Command, args []string) error {
	r.executing = cmd
	mode := r.effectiveRendering(cmd)
	r.selectedJSON = mode
	if mode == cli.JSONNone {
		report.SetOutput(cmd.ErrOrStderr())
	} else {
		// Machine mode owns the streams: no human notice may join the
		// result or error document.
		report.SetOutput(io.Discard)
	}
	if err := r.configureLogging(cmd); err != nil {
		return err
	}
	r.loggingReady = true
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
		if mode == cli.JSONNone && !r.contracts.Ignores(cmd) {
			report.Warn(fmt.Errorf(
				"lucy is on pre-alpha, it is not intended to be used in production environments and may break your server",
			))
		}
	}
	return nil
}

func (r *Runner) configureLogging(cmd *cobra.Command) error {
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
	if r.effectiveRendering(cmd) != cli.JSONNone {
		printLogs = false
	}
	return log.Configure(path, level, printLogs)
}

// applyAgentDefaults makes the non-interactive and compact-JSON behaviors
// implicit for detected agents. Explicit flags still win because parsing runs
// after these defaults are installed.
func (r *Runner) applyAgentDefaults() {
	r.setPersistentBoolDefault(cli.FlagNoInteractive, false)
	r.setPersistentBoolDefault(cli.FlagJSONCompact, false)
	if !agent.Detected() {
		return
	}
	r.setPersistentBoolDefault(cli.FlagNoInteractive, true)
	if !r.contracts.Supports(r.invocationCommand()) {
		return
	}
	if cli.JSONFlagsPresent(r.args) {
		return
	}
	r.setPersistentBoolDefault(cli.FlagJSONCompact, true)
}

func (r *Runner) setPersistentBoolDefault(name string, value bool) {
	flag := r.root.PersistentFlags().Lookup(name)
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

// injectAgentInstructions adds the agent notice to every help text. Help is
// rendered from Long, so this works for both fang and Cobra help paths.
func (r *Runner) injectAgentInstructions() {
	if r.agentHelpInjected || !agent.Detected() {
		return
	}
	r.agentHelpInjected = true
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
	walk(r.root)
}

// effectiveRendering reports the rendering requested by the actual
// invocation. Parsed flags win; otherwise the raw command line is inspected,
// so a failure that happens before Cobra parses flags still honors --json and
// --json-compact.
func (r *Runner) effectiveRendering(cmd *cobra.Command) cli.JSONRendering {
	if r.contracts.Ignores(cmd) {
		return cli.JSONNone
	}
	mode := cli.JSONModeFromArgs(cmd, r.args)
	if cmd.Flags().Parsed() && !r.flagParsingFailed {
		mode = cli.JSONMode(cmd)
	} else if mode == cli.JSONNone && !cli.JSONFlagsPresent(r.args) {
		if compact, _ := cmd.Root().PersistentFlags().GetBool(cli.FlagJSONCompact); compact {
			mode = cli.JSONCompact
		}
	}
	if mode == cli.JSONNone && r.contracts.IsNative(cmd) {
		return cli.JSONPretty
	}
	return mode
}

// invocationCommand finds the command the raw command line addresses, even
// when Cobra stopped before recording the executing command.
func (r *Runner) invocationCommand() *cobra.Command {
	if r.executing != nil {
		return r.executing
	}
	command, _, _ := r.root.Find(r.args)
	if command != nil {
		return command
	}
	return r.root
}

// renderError keeps human failures readable: structured details render their
// own multi-line explanation, ordinary errors render on one line, and usage
// mistakes keep the framework's format with its --help hint. Machine mode
// defers rendering to the CLI boundary so the log file can close first and a
// close failure joins the same document.
func (r *Runner) renderError(w io.Writer, styles fang.Styles, err error) {
	if err == nil {
		return
	}
	var exitErr *cli.ExitError
	if errors.As(err, &exitErr) && exitErr.Silent {
		return
	}
	command := r.invocationCommand()
	if mode := r.effectiveRendering(command); mode != cli.JSONNone {
		r.selectedJSON = mode
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
		// The framework names the rejected flag but never what this command
		// accepts, which is the fact the caller needs to retry.
		if hint := cli.UsageHint(command, err); hint != "" {
			fmt.Fprintf(w, "  %s %s\n", style.Note("hint:"), hint)
		}
		return
	}
	report.Error(err)
	fmt.Fprintf(w, "  %s %s\n", style.Muted("log:"), log.FilePath())
}

// WriteError renders a failure through the same mode-aware funnel outside
// command execution. main uses it for a log file that fails to close.
func (r *Runner) WriteError(err error) {
	if err == nil {
		return
	}
	command := r.invocationCommand()
	if mode := r.effectiveRendering(command); mode != cli.JSONNone {
		r.selectedJSON = mode
	}
	output := cli.NewOperationOutput(command)
	output.SetJSONRendering(r.selectedJSON)
	output.WriteError(err, log.FilePath())
}

// JSONSelected reports whether the actual invocation selected machine output.
func (r *Runner) JSONSelected() bool {
	return r.selectedJSON != cli.JSONNone
}

// FlushOutput publishes a machine result only after command cleanup succeeds.
func (r *Runner) FlushOutput() error {
	if !r.JSONSelected() || r.resultWriter == nil {
		return nil
	}
	if _, err := r.machineResult.WriteTo(r.resultWriter); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	return nil
}

// panicError reports an unexpected panic through the ordinary failure
// funnel. The message stays stable for machine output; the stack goes to the
// diagnostic log.
type panicError struct {
	value any
	stack []byte
}

func (e *panicError) Error() string {
	return fmt.Sprintf("internal error: %v", e.value)
}

// Execute runs the invocation. Per-run state is reset first, so a runner
// always starts from a clean parse result and output buffer.
func (r *Runner) Execute() (err error) {
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		stack := debug.Stack()
		err = &panicError{value: recovered, stack: stack}
		path := r.root.CommandPath()
		if r.executing != nil {
			path = r.executing.CommandPath()
		}
		log.Error(fmt.Errorf("%s panicked: %v\n%s", path, recovered, stack))
	}()
	displayVersion, displayCommit := buildVersion()
	r.executing = nil
	r.loggingReady = false
	r.flagParsingFailed = false
	r.applyAgentDefaults()
	r.injectAgentInstructions()
	command := r.invocationCommand()
	r.selectedJSON = r.effectiveRendering(command)
	r.root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		r.flagParsingFailed = true
		return err
	})
	r.machineResult.Reset()
	r.resultWriter = nil
	if r.JSONSelected() {
		r.resultWriter = r.root.OutOrStdout()
		r.root.SetOut(&r.machineResult)
		defer r.root.SetOut(r.resultWriter)
	}
	if r.JSONSelected() {
		// Fang probes terminal colors before invoking its error handler.
		// Machine mode must not emit those queries on the real stdout.
		r.root.SilenceErrors = true
		r.root.SilenceUsage = true
		r.root.Version = displayVersion
		_, err = r.root.ExecuteContextC(context.Background())
	} else {
		err = fang.Execute(
			context.Background(),
			r.root,
			fang.WithVersion(displayVersion),
			fang.WithCommit(displayCommit),
			fang.WithErrorHandler(r.renderError),
		)
	}
	if err != nil {
		command := r.executing
		if command == nil {
			var findErr error
			command, _, findErr = r.root.Find(r.args)
			if command == nil {
				command = r.root
			}
			if findErr != nil {
				// Unknown commands fail before Cobra parses the persistent
				// flags. Keep the flag failure for the log without rendering a
				// second human error.
				if flagErr := r.root.ParseFlags(r.args); flagErr != nil {
					err = errors.Join(err, flagErr)
				}
			}
			if configureErr := r.configureLogging(command); configureErr != nil {
				if !r.JSONSelected() {
					report.Error(configureErr)
				}
				return errors.Join(err, configureErr)
			}
			r.loggingReady = true
		}
		if r.loggingReady {
			var exitErr *cli.ExitError
			if !errors.As(err, &exitErr) || !exitErr.Silent {
				log.Error(fmt.Errorf("%s failed: %w", command.CommandPath(), err))
			}
		}
		return err
	}
	if r.loggingReady && r.executing != nil {
		if n := r.executing.Name(); n != "__complete" && n != "__completeNoDesc" {
			log.Info("action completed", "action", r.executing.CommandPath())
		}
	}
	return nil
}
