package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"charm.land/huh/v2"
	"github.com/mclucy/lucy/config"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/internal/cli/help"
	"github.com/mclucy/lucy/operation"
	serverpkg "github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/terminal/report"
	versionpkg "github.com/mclucy/lucy/version"
	"github.com/mclucy/lucy/workspace/discovery"
	"github.com/spf13/cobra"
)

const (
	flagInitForceName       = "force"
	flagInitNameName        = "name"
	flagInitMCDRVersionName = "mcdr-version"
	flagInitCoreName        = "core"
	flagInitCoreVersionName = "core-version"
	flagInitMinecraftName   = "minecraft"
	flagInitNoCommentsName  = "no-comments"
)

func NewInitCommand(
	contracts *JSONContracts,
	inv *Invocation,
	deps *Dependencies,
) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: help.InitShort,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return actionInit(cmd, inv, deps, args)
		},
	}
	initJSON.Bind(contracts, cmd)
	cmd.Flags().BoolP(
		flagInitForceName,
		"f",
		false,
		"Overwrite an existing manifest without asking",
	)
	cmd.Flags().String(
		flagInitNameName,
		"",
		"Server name to record (default: the directory name, or the existing name with --force)",
	)
	cmd.Flags().String(
		flagInitMCDRVersionName,
		"",
		"MCDReforged version to record",
	)
	cmd.Flags().String(
		flagInitCoreName,
		"",
		"Expect this server product; no match fails and lists the observed runtimes",
	)
	cmd.Flags().String(
		flagInitCoreVersionName,
		"",
		"Expect this exact server product version",
	)
	cmd.Flags().String(
		flagInitMinecraftName,
		"",
		"Expect this exact Minecraft version",
	)
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

func actionInit(
	cmd *cobra.Command,
	inv *Invocation,
	deps *Dependencies,
	_ []string,
) error {
	ctx, stop := CommandContext(cmd)
	defer stop()
	name := flagStringInit(cmd, flagInitNameName)
	if cmd.Flags().Changed(flagInitNameName) {
		if _, err := document.NormalizeName(name); err != nil {
			return fmt.Errorf("--%s: %w", flagInitNameName, err)
		}
	}
	expectation, err := initExpectation(cmd)
	if err != nil {
		return err
	}
	opts := operation.InitOptions{
		Name:        name,
		MCDRVersion: flagStringInit(cmd, flagInitMCDRVersionName),
		Expectation: expectation,
		Force:       flagBoolInit(cmd, flagInitForceName),
		NoComments:  flagBoolInit(cmd, flagInitNoCommentsName),
	}
	return initializeWorkspace(ctx, cmd, inv, deps, opts)
}

// initializeWorkspace initializes one workspace and owns its presentation.
// Warnings observed during initialization surface on stderr once, failures
// return to the command framework, and success reports the files written from
// the returned document instead of rereading them.
func initializeWorkspace(
	ctx context.Context,
	cmd *cobra.Command,
	inv *Invocation,
	deps *Dependencies,
	opts operation.InitOptions,
) error {
	output := newInstallOutput(cmd, inv)
	defer output.Close()
	opts.Trace = output.Trace()
	opts.PromptRuntime = func(observed discovery.Result) (*discovery.Installation, error) {
		output.Close()
		return promptRuntime(ctx, cmd, inv, observed)
	}
	result, err := deps.Operations.Initialize(ctx, inv.Directory, opts)
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
// report. The commands name observed installations by identity.
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
	var commands []string
	switch selectionErr.State {
	case discovery.SelectionAmbiguous:
		detail.Title = "several server runtimes share this directory"
		commands = installationCommands(selectionErr.Installations)
	case discovery.SelectionUnmatched:
		detail.Title = "no installed server matches the requested filters"
		detail.Lines = observedInstallationLines(selectionErr.Installations)
	case discovery.SelectionMissingDeclared:
		detail.Title = "the declared server installation is missing"
	case discovery.SelectionInvalid:
		detail.Title = "no launchable server installation was found"
	default:
		return err
	}
	detail.Lines = append(detail.Lines, commands...)
	detail.Lines = append(detail.Lines, unreadable...)
	if len(commands) > 0 {
		detail.Hint = "Run one of these commands to select the server to initialize."
	} else if selectionErr.State == discovery.SelectionUnmatched {
		detail.Hint = "Omit --core, --core-version, and --minecraft to choose interactively, or pass values that match an installed server."
	}
	return detail
}

func installationCommands(installations []discovery.Installation) []string {
	var commands []string
	seen := map[string]bool{}
	for i := range installations {
		identity := installations[i].Identity
		if !identity.IsValid() {
			continue
		}
		command := identitySelectionCommand(identity)
		if command == "" || seen[command] {
			continue
		}
		seen[command] = true
		commands = append(commands, command)
	}
	return commands
}

func observedInstallationLines(installations []discovery.Installation) []string {
	lines := make([]string, 0, len(installations))
	seen := map[string]bool{}
	for i := range installations {
		identity := installations[i].Identity
		if !identity.IsValid() {
			continue
		}
		label := installationIdentityLabel(identity)
		if seen[label] {
			continue
		}
		seen[label] = true
		lines = append(lines, label)
	}
	return lines
}

// initExpectation builds the invocation filter from the selection flags. It
// never supplies facts; discovery still has to observe a matching runtime.
func initExpectation(cmd *cobra.Command) (*discovery.Expectation, error) {
	core := strings.TrimSpace(flagStringInit(cmd, flagInitCoreName))
	coreVersion, err := concreteInitVersion(cmd, flagInitCoreVersionName)
	if err != nil {
		return nil, err
	}
	minecraft, err := concreteInitVersion(cmd, flagInitMinecraftName)
	if err != nil {
		return nil, err
	}
	if core == "" && coreVersion == "" && minecraft == "" {
		return nil, nil
	}
	if coreVersion != "" && core == "" {
		return nil, fmt.Errorf("--%s needs --%s", flagInitCoreVersionName, flagInitCoreName)
	}
	product, known := serverpkg.ParseProduct(core)
	if !known {
		product = serverpkg.Product(strings.ToLower(core))
	}
	return &discovery.Expectation{
		Product:     product,
		Minecraft:   minecraft,
		CoreVersion: coreVersion,
	}, nil
}

func concreteInitVersion(
	cmd *cobra.Command,
	name string,
) (versionpkg.Version, error) {
	raw := strings.TrimSpace(flagStringInit(cmd, name))
	if raw == "" {
		return "", nil
	}
	value := versionpkg.Version(raw)
	if value.IsInvalid() || value.CanInfer() {
		return "", fmt.Errorf("--%s expects an exact version, got %q", name, raw)
	}
	return value, nil
}

func promptRuntime(
	ctx context.Context,
	cmd *cobra.Command,
	inv *Invocation,
	observed discovery.Result,
) (*discovery.Installation, error) {
	// A closed gate returns no selection, so Initialize reports the typed
	// ambiguous-selection error whose commands name identities exactly.
	if !NewPromptGate(cmd, inv).Available() {
		return nil, nil
	}
	options := make([]huh.Option[int], 0, len(observed.Installations))
	for i := range observed.Installations {
		installation := &observed.Installations[i]
		if !installation.Identity.IsValid() {
			continue
		}
		options = append(options, huh.NewOption(
			installationIdentityLabel(installation.Identity),
			i,
		))
	}
	if len(options) == 0 {
		return nil, nil
	}
	selected := -1
	form := huh.NewForm(huh.NewGroup(huh.NewSelect[int]().
		Title("Select the server to initialize").
		Description("Use --core, --core-version, and --minecraft to make this selection without a prompt.").
		Options(options...).Value(&selected))).
		WithInput(cmd.InOrStdin()).WithOutput(cmd.ErrOrStderr())
	if err := NormalizePromptError(ctx, form.RunWithContext(ctx)); err != nil {
		return nil, fmt.Errorf("server selection cancelled: %w", err)
	}
	if selected < 0 || selected >= len(observed.Installations) {
		return nil, nil
	}
	return &observed.Installations[selected], nil
}

func flagStringInit(cmd *cobra.Command, name string) string {
	value, _ := cmd.Flags().GetString(name)
	return value
}

func flagBoolInit(cmd *cobra.Command, name string) bool {
	value, _ := cmd.Flags().GetBool(name)
	return value
}
