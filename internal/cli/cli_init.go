package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"charm.land/huh/v2"
	"github.com/mclucy/lucy/config"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/internal/cli/help"
	"github.com/mclucy/lucy/operation"
	"github.com/mclucy/lucy/report"
	"github.com/mclucy/lucy/workspace/discovery"
	"github.com/spf13/cobra"
)

const (
	flagInitForceName       = "force"
	flagInitMCDRVersionName = "mcdr-version"
	flagInitEntryPointName  = "entry-point"
	flagInitNoCommentsName  = "no-comments"
)

func NewInitCommand(contracts *JSONContracts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: help.InitShort,
		Args:  cobra.NoArgs,
		RunE:  actionInit,
	}
	initJSON.Bind(contracts, cmd)
	cmd.Flags().BoolP(
		flagInitForceName,
		"f",
		false,
		"Overwrite an existing manifest without asking",
	)
	cmd.Flags().String(
		flagInitMCDRVersionName,
		"",
		"MCDReforged version to record",
	)
	cmd.Flags().String(flagInitEntryPointName, "", "Server entry point to adopt when several runtimes are present")
	_ = cmd.MarkFlagFilename(flagInitEntryPointName, "jar")
	cmd.Flags().Bool(
		flagInitNoCommentsName,
		false,
		"Write generated manifests and configuration without explanatory comments",
	)
	return cmd
}

// initJSON is the machine-readable result of `lucy init`.
var initJSON JSONResult[*initResult]

// initResult adds workspace configuration publication to the install result
// that other operations share.
type initResult struct {
	*install.Result
	PublishedConfig bool `json:"published_config"`
}

func actionInit(cmd *cobra.Command, _ []string) error {
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("could not determine working directory: %w", err)
	}
	ctx, stop := CommandContext(cmd)
	defer stop()
	opts := operation.InitOptions{
		MCDRVersion: flagStringInit(cmd, flagInitMCDRVersionName),
		EntryPoint:  flagStringInit(cmd, flagInitEntryPointName),
		Force:       flagBoolInit(cmd, flagInitForceName),
		NoComments:  flagBoolInit(cmd, flagInitNoCommentsName),
	}
	return initializeWorkspace(ctx, cmd, workDir, opts)
}

// initializeWorkspace initializes one workspace and owns its presentation.
// Warnings observed during initialization surface on stderr once, failures
// return to the command framework, and success reports the files written from
// the returned document instead of rereading them.
func initializeWorkspace(ctx context.Context, cmd *cobra.Command, workDir string, opts operation.InitOptions) error {
	output := newInstallOutput(cmd)
	defer output.Close()
	opts.Trace = output.Trace()
	opts.PromptRuntime = func(observed discovery.Result) (string, error) {
		output.Close()
		return promptRuntime(ctx, cmd, observed)
	}
	result, err := operation.Initialize(ctx, workDir, opts)
	if err != nil {
		return output.Failure(initializationFailure(err, result))
	}
	output.Close()
	for _, warning := range result.Warnings {
		output.Warn(warning)
	}
	reportDiscoveryFindings(&result.Discovery, output.Output)
	if output.JSON() {
		return initJSON.Write(output.Output, &initResult{
			Result: &install.Result{
				Schema: install.ResultSchema,
				Kind:   "install",
				Mode:   install.ModeResolve,
				Server: install.ServerChange{
					Product:   result.Lock.Server.Product,
					Minecraft: result.Lock.Server.Minecraft,
					Version:   result.Lock.Server.CoreVersion,
				},
				Packages:          []install.PackageChange{},
				Phases:            []install.Phase{},
				TotalPackages:     len(result.Lock.Packages),
				PublishedManifest: true,
				PublishedLock:     result.PublishedLock,
				WarningMessages:   output.Warnings(),
			},
			PublishedConfig: result.PublishedConfig,
		})
	}
	written := []string{document.ManifestFilename}
	if result.PublishedLock {
		written = append(written, document.LockFilename)
	}
	if result.PublishedConfig {
		written = append(written, config.WorkspaceFilename)
	}
	fmt.Fprintf(
		output.Human(),
		"Wrote %s for Minecraft %s (%s)\n  Recorded %s\n",
		writtenFilesLabel(written),
		result.Document.Server.Minecraft,
		result.Document.Server.Core.Product,
		packageCountLabel(len(result.Lock.Packages)),
	)
	return nil
}

// writtenFilesLabel names the files a command wrote in running prose.
func writtenFilesLabel(paths []string) string {
	if len(paths) < 2 {
		return strings.Join(paths, "")
	}
	return strings.Join(paths[:len(paths)-1], ", ") + " and " + paths[len(paths)-1]
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
		detail.Title = "the declared server installation is missing"
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

func promptRuntime(ctx context.Context, cmd *cobra.Command, observed discovery.Result) (string, error) {
	// A closed gate returns no selection, so Initialize reports the typed
	// ambiguous-selection error whose commands name --entry-point exactly.
	if !NewPromptGate(cmd).Available() {
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
	if err := NormalizePromptError(ctx, form.RunWithContext(ctx)); err != nil {
		return "", fmt.Errorf("server selection cancelled: %w", err)
	}
	return selected, nil
}

func flagStringInit(cmd *cobra.Command, name string) string {
	value, _ := cmd.Flags().GetString(name)
	return value
}

func flagBoolInit(cmd *cobra.Command, name string) bool {
	value, _ := cmd.Flags().GetBool(name)
	return value
}
