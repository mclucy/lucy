package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"runtime/debug"
	"strings"

	"charm.land/fang/v2"
	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/config"
	"github.com/mclucy/lucy/filetrack"
	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/internal/agent"
	"github.com/mclucy/lucy/internal/cipher"
	"github.com/mclucy/lucy/internal/cli"
	"github.com/mclucy/lucy/internal/cli/compile"
	docscli "github.com/mclucy/lucy/internal/cli/docs"
	"github.com/mclucy/lucy/internal/cli/file"
	"github.com/mclucy/lucy/internal/cli/info"
	"github.com/mclucy/lucy/internal/cli/search"
	"github.com/mclucy/lucy/javainstall"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/operation"
	"github.com/mclucy/lucy/report"
	"github.com/mclucy/lucy/serverregistry"
	"github.com/mclucy/lucy/terminal/style"
	"github.com/mclucy/lucy/upstream/routing"
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

// RunnerOptions supplies the invocation directory, streams, and network transport.
// Directory must be absolute. A nil HTTPClient creates a client owned by the run.
type RunnerOptions struct {
	Directory  string
	Stdin      io.Reader
	Stdout     io.Writer
	Stderr     io.Writer
	HTTPClient *http.Client
}

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
	invocation        *cli.Invocation
	dependencies      *cli.Dependencies
	httpClient        *http.Client
	ownsHTTPClient    bool
	directory         string
}

// NewRunner prepares an isolated invocation with a fresh command tree, fresh
// JSON contracts, and the given streams.
func NewRunner(args []string, options RunnerOptions) *Runner {
	r := &Runner{
		contracts:    cli.NewJSONContracts(),
		args:         args,
		stdin:        options.Stdin,
		stdout:       options.Stdout,
		stderr:       options.Stderr,
		invocation:   cli.NewInvocation(options.Directory, nil),
		dependencies: &cli.Dependencies{},
		httpClient:   options.HTTPClient,
		directory:    options.Directory,
	}
	if r.httpClient == nil {
		transport, ok := http.DefaultTransport.(*http.Transport)
		if !ok {
			transport = &http.Transport{Proxy: http.ProxyFromEnvironment}
		}
		r.httpClient = &http.Client{Transport: transport.Clone()}
		r.ownsHTTPClient = true
	}
	r.root = NewRootCommand(r.contracts, r.invocation, r.dependencies)
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
func NewRootCommand(contracts *cli.JSONContracts, invocation *cli.Invocation, dependencies *cli.Dependencies) *cobra.Command {
	root := &cobra.Command{
		Use:   "lucy",
		Short: "The Minecraft server package manager",
	}
	cli.RegisterServerSelection(root)
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

	docs := docscli.NewCommand()
	contracts.Ignore(docs)
	root.AddCommand(
		cli.NewAddCommand(contracts, invocation, dependencies),
		cli.NewBisectCommand(contracts, invocation, dependencies),
		cli.NewCacheCommand(contracts, invocation, dependencies),
		cli.NewConfigCommand(contracts, invocation, dependencies),
		cli.NewCreateCommand(contracts, invocation, dependencies),
		cli.NewDeinitCommand(contracts, invocation, dependencies),
		docs,
		cli.NewDoctorCommand(),
		file.NewCommand(contracts, invocation, dependencies),
		cli.NewExportCommand(invocation),
		cli.NewInitCommand(contracts, invocation, dependencies),
		cli.NewInstallCommand(contracts, invocation, dependencies),
		cli.NewJavaCommand(contracts, invocation, dependencies),
		cli.NewLeavesCommand(contracts, invocation, dependencies),
		cli.NewRemoveCommand(contracts, invocation, dependencies),
		cli.NewServerCommand(contracts, invocation, dependencies),
		cli.NewTreeCommand(contracts, invocation, dependencies),
		cli.NewUpgradeCommand(contracts, invocation, dependencies),
		compile.NewCommand(invocation),
		info.NewCommand(contracts, invocation, dependencies),
		cli.NewStatusCommand(contracts, invocation, dependencies),
		search.NewCommand(contracts, invocation, dependencies),
		cli.NewSchemaCommand(contracts, invocation),
	)
	return root
}

func (r *Runner) persistentPreRunE(cmd *cobra.Command, args []string) error {
	r.executing = cmd
	mode := r.effectiveRendering(cmd)
	r.selectedJSON = mode
	r.configurePresentation(cmd)
	if !filepath.IsAbs(r.directory) {
		return fmt.Errorf("invocation directory must be absolute")
	}
	if err := r.configureLogging(cmd); err != nil {
		return err
	}
	r.loggingReady = true
	if err := r.configureServices(cmd); err != nil {
		return err
	}
	if n := cmd.Name(); n != "__complete" && n != "__completeNoDesc" {
		r.invocation.Logger.Info("action invoked", "action", cmd.CommandPath(), "directory", r.invocation.Directory)
		if mode == cli.JSONNone && !r.contracts.Ignores(cmd) {
			report.New(cmd.ErrOrStderr(), r.invocation.Theme).Warn(fmt.Errorf(
				"lucy is on pre-alpha, it is not intended to be used in production environments and may break your server",
			))
		}
	}
	return nil
}

func (r *Runner) configurePresentation(cmd *cobra.Command) {
	noStyle, _ := cmd.Flags().GetBool(cli.FlagNoStyle)
	noColor, _ := cmd.Flags().GetBool(cli.FlagNoColor)
	noInteractive, _ := cmd.Flags().GetBool(cli.FlagNoInteractive)
	r.invocation.Theme = style.New(!noStyle && !noColor)
	r.invocation.Interactive = r.effectiveRendering(cmd) == cli.JSONNone && !noStyle && !noInteractive
	if r.effectiveRendering(cmd) == cli.JSONNone {
		var input io.Reader
		if r.invocation.Interactive {
			input = r.stdin
		}
		r.invocation.Stdout, r.invocation.Stderr = style.DetectTerminals(input, r.stdout, r.stderr)
	}
}

func (r *Runner) configureServices(cmd *cobra.Command) error {
	registry, registryErr := serverregistry.New()
	r.dependencies.Registry = registry
	r.dependencies.RegistryError = registryErr
	if cmd.Name() != "__complete" && cmd.Name() != "__completeNoDesc" &&
		cmd.Root().PersistentFlags().Changed(cli.FlagServer) && registryErr != nil {
		return registryErr
	}
	if err := cli.ResolveServer(cmd, registry, r.invocation); err != nil {
		return err
	}
	settings, err := config.New(r.invocation.Directory, r.invocation.Logger)
	if err != nil {
		return err
	}
	proxy, proxyErr := settings.GitHubProxy()
	key, keyErr := settings.CurseForgeAPIKey()
	if err := errors.Join(proxyErr, keyErr); err != nil {
		if !canRepairSettings(cmd) {
			return fmt.Errorf("%w\nrun `lucy config` to inspect or repair machine settings", err)
		}
		r.invocation.Logger.Warn(err)
	}
	key = strings.TrimSpace(key)
	configuredKey := key != ""
	if key == "" {
		embedded, err := cipher.Decode()
		if err != nil {
			r.invocation.Logger.Debug("embedded CurseForge key unavailable", "error", err)
		} else {
			key = strings.TrimSpace(embedded)
		}
	}
	r.invocation.Logger.Debug(
		"machine settings applied",
		"github_proxy_configured", proxy != "",
		"curseforge_key_configured", configuredKey,
	)
	network := cache.New(cache.DefaultRoot(), cache.DefaultCacheConfig(), r.httpClient, r.invocation.Logger)
	router := routing.New(network, r.invocation.Logger, key)
	files := filetrack.NewService(r.httpClient, r.invocation.Logger)
	installer := install.NewService(
		router, network, files, install.NewPythonHost(r.invocation.Directory, r.invocation.Logger), r.invocation.Logger, proxy,
	)
	r.dependencies.Network = network
	r.dependencies.Router = router
	r.dependencies.Files = files
	r.dependencies.Operations = operation.NewService(installer, router, registry, r.invocation.Logger)
	r.dependencies.JavaHost = javainstall.NewHost(network, javainstall.NewSystem(), r.invocation.Logger)
	return nil
}

// canRepairSettings reports whether cmd belongs to the surfaces that stay
// runnable while machine settings are unreadable, so the user can inspect and
// repair the configuration that fails the other commands.
func canRepairSettings(cmd *cobra.Command) bool {
	for current := cmd; current != nil; current = current.Parent() {
		switch current.Name() {
		case "config", "docs", "doctor", "schema",
			"__complete", "__completeNoDesc":
			return true
		}
	}
	return false
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
	var mirror io.Writer
	if printLogs && r.effectiveRendering(cmd) == cli.JSONNone {
		mirror = r.stderr
	}
	if path != "" && !filepath.IsAbs(path) {
		path = filepath.Join(r.directory, path)
	}
	if err := r.invocation.Logger.Close(); err != nil {
		return err
	}
	r.invocation.Logger, err = log.New(path, level, mirror)
	return err
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
		report.RenderDetail(w, detail, r.invocation.Theme)
		r.writeLogPath(w)
		return
	}
	if cli.IsFrameworkUsageError(err) {
		fang.DefaultErrorHandler(w, styles, err)
		// The framework names the rejected flag but never what this command
		// accepts, which is the fact the caller needs to retry.
		if hint := cli.UsageHint(command, err); hint != "" {
			fmt.Fprintf(w, "  %s %s\n", r.invocation.Theme.Note("hint:"), hint)
		}
		return
	}
	report.New(w, r.invocation.Theme).Error(err)
	r.writeLogPath(w)
}

func (r *Runner) writeLogPath(w io.Writer) {
	if path := r.invocation.Logger.FilePath(); path != "" {
		fmt.Fprintf(w, "  %s %s\n", r.invocation.Theme.Muted("log:"), path)
	}
}

// Close releases the invocation's diagnostics and owned HTTP connections.
func (r *Runner) Close() error {
	var cacheErr error
	if r.dependencies.Network != nil {
		cacheErr = r.dependencies.Network.Close()
	}
	if r.ownsHTTPClient {
		r.httpClient.CloseIdleConnections()
	}
	return errors.Join(cacheErr, r.invocation.Logger.Close())
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
	output := cli.NewOperationOutput(command, r.invocation)
	output.SetJSONRendering(r.selectedJSON)
	output.WriteError(err, r.invocation.Logger.FilePath())
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
func (r *Runner) Execute(ctx context.Context) (err error) {
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
		r.invocation.Logger.Error(fmt.Errorf("%s panicked: %v\n%s", path, recovered, stack))
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
		_, err = r.root.ExecuteContextC(ctx)
	} else {
		err = fang.Execute(
			ctx,
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
					report.New(r.stderr, r.invocation.Theme).Error(configureErr)
				}
				return errors.Join(err, configureErr)
			}
			r.loggingReady = true
		}
		if r.loggingReady {
			var exitErr *cli.ExitError
			if !errors.As(err, &exitErr) || !exitErr.Silent {
				r.invocation.Logger.Error(fmt.Errorf("%s failed: %w", command.CommandPath(), err))
			}
		}
		return err
	}
	if r.loggingReady && r.executing != nil {
		if n := r.executing.Name(); n != "__complete" && n != "__completeNoDesc" {
			r.invocation.Logger.Info("action completed", "action", r.executing.CommandPath())
		}
	}
	return nil
}
