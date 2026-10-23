package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"charm.land/huh/v2"
	"github.com/charmbracelet/x/term"
	"github.com/mclucy/lucy/internal/cli/help"
	"github.com/mclucy/lucy/lockfile"
	"github.com/mclucy/lucy/manifest"
	"github.com/mclucy/lucy/operation"
	"github.com/mclucy/lucy/report"
	"github.com/mclucy/lucy/terminal/style"
	"github.com/mclucy/lucy/workspace/discovery"
	"github.com/spf13/cobra"
)

const (
	flagInitForceName       = "force"
	flagInitMCDRVersionName = "mcdr-version"
	flagInitEntryPointName  = "entry-point"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: help.InitShort,
	Args:  cobra.NoArgs,
	RunE:  actionInit,
}

func NewInitCommand() *cobra.Command {
	initCmd.Flags().BoolP(
		flagInitForceName,
		"f",
		false,
		"Overwrite an existing manifest without asking",
	)
	initCmd.Flags().String(
		flagInitMCDRVersionName,
		"",
		"MCDReforged version to record",
	)
	initCmd.Flags().String(flagInitEntryPointName, "", "Server entry point to adopt when several runtimes are present")
	_ = initCmd.MarkFlagFilename(flagInitEntryPointName, "jar")
	return initCmd
}

func actionInit(cmd *cobra.Command, _ []string) error {
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("could not determine working directory: %w", err)
	}
	opts := operation.InitOptions{
		MCDRVersion: flagStringInit(cmd, flagInitMCDRVersionName),
		EntryPoint:  flagStringInit(cmd, flagInitEntryPointName),
		Force:       flagBoolInit(cmd, flagInitForceName),
		Detect:      true,
	}
	return initializeWorkspace(cmd, workDir, opts)
}

// initializeWorkspace initializes one workspace and owns its presentation.
// Warnings observed during initialization surface on stderr once, failures
// return to the command framework, and success reports the files written from
// the returned document instead of rereading them.
func initializeWorkspace(cmd *cobra.Command, workDir string, opts operation.InitOptions) error {
	output := newInstallOutput(cmd)
	defer output.Close()
	opts.Trace = output.Trace()
	opts.PromptRuntime = func(observed discovery.Result) (string, error) {
		output.Close()
		return promptRuntime(cmd, observed)
	}
	ctx, stop := commandContext(cmd)
	defer stop()
	result, err := operation.Initialize(ctx, workDir, opts)
	if err != nil {
		return output.Failure(initializationFailure(err, result))
	}
	output.Close()
	for _, warning := range result.Warnings {
		report.Warn(warning)
	}
	reportDiscoveryFindings(&result.Discovery)
	fmt.Fprintf(
		cmd.OutOrStdout(),
		"Wrote %s and %s for Minecraft %s (%s)\n",
		manifest.Filename,
		lockfile.Filename,
		result.Document.Server.Minecraft,
		result.Document.Server.Core.Product,
	)
	return nil
}

// initializationFailure turns a typed selection failure into an actionable
// report. The commands name installed entry points exactly.
func initializationFailure(err error, result *operation.InitResult) error {
	var unreadable []string
	if result != nil {
		unreadable = unreadableFindingLines(&result.Discovery)
	}
	var selectionErr *discovery.SelectionError
	if !errors.As(err, &selectionErr) {
		if len(unreadable) == 0 {
			return err
		}
		return &report.Detail{
			Title: err.Error(),
			Lines: unreadable,
			Cause: err,
		}
	}
	detail := &report.Detail{Cause: err}
	switch selectionErr.State {
	case discovery.SelectionAmbiguous:
		detail.Title = "several server runtimes share this directory"
	case discovery.SelectionMissingDeclared:
		detail.Title = "the declared server entry point is missing"
	case discovery.SelectionInvalid:
		detail.Title = "the requested entry point is not a recognized server"
	default:
		return err
	}
	commands := entryPointCommands(selectionErr.Installations)
	detail.Lines = append(detail.Lines, commands...)
	detail.Lines = append(detail.Lines, unreadable...)
	if len(commands) > 0 {
		detail.Hint = "Run one of these commands to select the server to initialize."
	}
	return detail
}

func entryPointCommands(installations []discovery.Installation) []string {
	var commands []string
	seen := map[string]bool{}
	for i := range installations {
		for file := range installations[i].EntryPoints() {
			if file.Path == "" || seen[file.Path] {
				continue
			}
			seen[file.Path] = true
			commands = append(commands, "lucy init --entry-point '"+strings.ReplaceAll(file.Path, "'", "'\\''")+"'")
		}
	}
	return commands
}

func promptRuntime(cmd *cobra.Command, observed discovery.Result) (string, error) {
	stdin, ok := cmd.InOrStdin().(*os.File)
	if !ok || !term.IsTerminal(stdin.Fd()) || !style.InteractiveEnabled() {
		return "", nil
	}
	options := make([]huh.Option[string], 0, len(observed.Installations))
	for i := range observed.Installations {
		installation := &observed.Installations[i]
		for file := range installation.EntryPoints() {
			options = append(options, huh.NewOption(
				fmt.Sprintf(
					"%s %s / Minecraft %s (%s)",
					installation.Identity.Product().Title(),
					installation.Identity.Primary.Version,
					installation.Identity.MinecraftVersion(),
					file.Path,
				),
				file.Path,
			))
		}
	}
	if len(options) == 0 {
		return "", nil
	}
	var selected string
	form := huh.NewForm(huh.NewGroup(huh.NewSelect[string]().
		Title("Select the server to initialize").
		Description("Use --entry-point <entry-point> to make this selection without a prompt.").
		Options(options...).Value(&selected))).
		WithInput(cmd.InOrStdin()).WithOutput(cmd.ErrOrStderr())
	if err := form.Run(); err != nil {
		return "", fmt.Errorf("server selection cancelled: %w", err)
	}
	return selected, nil
}

// promptMinecraft returns the interactive Minecraft-version prompt for
// Initialize. It requires a terminal stdin with interactive mode enabled,
// echoes the question on stderr, and returns the trimmed answer.
func promptMinecraft(cmd *cobra.Command) func() (string, error) {
	return func() (string, error) {
		stdin := cmd.InOrStdin()
		file, isFile := stdin.(*os.File)
		if !isFile || !term.IsTerminal(file.Fd()) || !style.InteractiveEnabled() {
			return "", errors.New("a concrete --minecraft version is required")
		}
		fmt.Fprint(cmd.ErrOrStderr(), "Minecraft version: ")
		// Piped answers may end without a newline, so a final line behind
		// io.EOF is still a valid answer.
		text, err := bufio.NewReader(stdin).ReadString('\n')
		if err == io.EOF && strings.TrimSpace(text) != "" {
			err = nil
		}
		if err != nil {
			return "", err
		}
		answer := strings.TrimSpace(text)
		if answer == "" {
			return "", errors.New("a concrete --minecraft version is required")
		}
		return answer, nil
	}
}

func flagStringInit(cmd *cobra.Command, name string) string {
	value, _ := cmd.Flags().GetString(name)
	return value
}

func flagBoolInit(cmd *cobra.Command, name string) bool {
	value, _ := cmd.Flags().GetBool(name)
	return value
}
