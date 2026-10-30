package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"

	"charm.land/huh/v2"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/cli/help"
	"github.com/mclucy/lucy/java"
	"github.com/mclucy/lucy/javaruntime"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/operation"
	"github.com/mclucy/lucy/report"
	"github.com/mclucy/lucy/terminal/style"
	"github.com/spf13/cobra"
)

const flagJavaDistributionName = "distribution"

// javaChoices lists the long-term support releases that the interactive form
// offers next to the installed major versions.
var javaChoices = []int{8, 11, 17, 21, 25}

var javaListJSON JSONResult[javaListView]

var javaUseJSON JSONResult[*operation.JavaChangeResult]

func NewJavaCommand(
	contracts *JSONContracts,
	inv *Invocation,
	deps *Dependencies,
) *cobra.Command {
	javaCmd := &cobra.Command{
		Use:   "java <subcommand>",
		Short: help.JavaShort,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	MarkHelpOnly(javaCmd)
	javaListCmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   help.JavaListShort,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return actionJavaList(cmd, inv, deps, args)
		},
	}
	javaUseCmd := &cobra.Command{
		Use:   "use [version|auto]",
		Short: help.JavaUseShort,
		Long:  help.JavaUseLong,
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return actionJavaUse(cmd, inv, deps, args)
		},
	}
	javaListJSON.Bind(contracts, javaListCmd)
	javaUseJSON.Bind(contracts, javaUseCmd)
	javaUseCmd.Flags().String(
		flagJavaDistributionName,
		"",
		"Pin a Java distribution in lucy.yaml, or auto to accept any",
	)
	javaCmd.AddCommand(javaListCmd, javaUseCmd)
	return javaCmd
}

// javaListView is the machine-readable `java list` result.
type javaListView struct {
	Requirement *javaruntime.Requirement `json:"requirement,omitempty"`
	Selection   *javaruntime.Check       `json:"selection,omitempty"`
	Runtimes    []javaRuntimeView        `json:"runtimes"`
}

type javaRuntimeView struct {
	javaruntime.Runtime
	Selected bool     `json:"selected"`
	Problems []string `json:"problems,omitempty"`
}

func actionJavaList(
	cmd *cobra.Command,
	inv *Invocation,
	deps *Dependencies,
	_ []string,
) error {
	root := inv.Directory
	ctx, stop := CommandContext(cmd)
	defer stop()
	requirement, err := lockedJavaRequirement(root, inv)
	if err != nil {
		return err
	}
	runtimes, err := javaruntime.Installed(ctx, inv.Logger)
	if err != nil {
		recordJavaTelemetryFailure(inv)
		return err
	}
	view := javaListView{Requirement: requirement, Runtimes: []javaRuntimeView{}}
	if requirement != nil {
		if view.Selection, err = javaruntime.Select(
			ctx,
			root,
			*requirement,
			inv.Logger,
		); err != nil {
			recordJavaTelemetryFailure(inv)
			return err
		}
	}
	for _, runtime := range runtimes {
		entry := javaRuntimeView{Runtime: runtime}
		if requirement != nil {
			entry.Problems = requirement.Misses(&runtime)
		}
		entry.Selected = view.Selection != nil && view.Selection.Runtime != nil &&
			view.Selection.Runtime.Home == runtime.Home
		view.Runtimes = append(view.Runtimes, entry)
	}
	recordJavaTelemetry(inv, view.Selection)
	output := NewQueryOutput(cmd, inv)
	if output.JSON() {
		return javaListJSON.Write(output, view)
	}
	out := output.Human()
	if requirement == nil {
		fmt.Fprintf(
			out,
			"No %s; run lucy install to resolve the Java the server asks for.\n",
			document.LockFilename,
		)
	} else {
		writeJavaRequirement(out, requirement, inv.Theme)
	}
	if view.Selection != nil && view.Selection.Source == javaruntime.SourceConfigured {
		fmt.Fprintf(out, "%s selects %s\n", view.Selection.ConfigPath, view.Selection.ConfiguredHome)
	}
	fmt.Fprintln(out)
	if len(view.Runtimes) == 0 {
		fmt.Fprintln(out, "No Java runtime found.")
	}
	for _, entry := range view.Runtimes {
		writeJavaRuntime(out, entry, requirement != nil, inv.Theme)
	}
	if view.Selection != nil {
		reporter := report.New(cmd.ErrOrStderr(), inv.Theme)
		for _, warning := range javaWarnings(view.Selection) {
			reporter.Warn(warning)
		}
	}
	return nil
}

func writeJavaRequirement(
	out io.Writer,
	requirement *javaruntime.Requirement,
	theme style.Theme,
) {
	target := strconv.Itoa(requirement.Target())
	fmt.Fprintf(
		out,
		"%s %s\n",
		theme.Accent("Requires"),
		javaSpecLabel(target, requirement.Distribution),
	)
	width := 0
	for _, c := range requirement.Constraints {
		width = max(width, len(c.Source))
	}
	for _, c := range requirement.Constraints {
		fmt.Fprintf(
			out,
			"  %-*s  %s\n",
			width,
			c.Source,
			theme.Muted(c.Expression),
		)
	}
}

func writeJavaRuntime(
	out io.Writer,
	entry javaRuntimeView,
	checked bool,
	theme style.Theme,
) {
	marker := " "
	switch {
	case !checked:
	case len(entry.Problems) == 0:
		marker = theme.Success("✓")
	default:
		marker = theme.Failure("✗")
	}
	line := fmt.Sprintf(
		"%s %-3d %-12s %-22s %s",
		marker,
		entry.Major,
		entry.Version,
		entry.Build(),
		theme.Muted(entry.Home),
	)
	if entry.Selected {
		line += " " + theme.Accent("(selected)")
	}
	fmt.Fprintln(out, line)
}

func actionJavaUse(
	cmd *cobra.Command,
	inv *Invocation,
	deps *Dependencies,
	args []string,
) error {
	root := inv.Directory
	ctx, stop := CommandContext(cmd)
	defer stop()
	change, err := javaChangeFromFlags(cmd, args)
	if err != nil {
		return err
	}
	if change == (operation.JavaChange{}) {
		if !NewPromptGate(cmd, inv).Available() {
			return fmt.Errorf(
				"specify a Java version, auto, or --%s; interactive selection is unavailable "+
					"with --no-interactive or without a terminal",
				flagJavaDistributionName,
			)
		}
		if change, err = promptJavaChange(ctx, root, NewPromptGate(cmd, inv), inv.Logger); err != nil {
			return err
		}
	}
	result, err := deps.Operations.SetJava(ctx, root, change)
	if err != nil {
		return err
	}
	output := NewOperationOutput(cmd, inv)
	if output.JSON() {
		return javaUseJSON.Write(output, result)
	}
	out := output.Human()
	fmt.Fprintf(out, "%s: %s\n", document.ManifestFilename, javaOverrideLabel(result.Java))
	if result.PublishedManifest {
		fmt.Fprintf(out, "Run lucy install to update %s.\n", document.LockFilename)
	}
	return nil
}

func javaChangeFromFlags(cmd *cobra.Command, args []string) (operation.JavaChange, error) {
	var change operation.JavaChange
	if len(args) == 1 {
		version, err := parseJavaMajor(args[0])
		if err != nil {
			return change, err
		}
		change.Version = &version
	}
	if cmd.Flags().Changed(flagJavaDistributionName) {
		raw, _ := cmd.Flags().GetString(flagJavaDistributionName)
		distribution, err := document.ParseJavaDistribution(raw)
		if err != nil {
			return change, err
		}
		change.Distribution = &distribution
	}
	return change, nil
}

func parseJavaMajor(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if strings.EqualFold(raw, document.JavaAuto) {
		return 0, nil
	}
	version, err := strconv.Atoi(raw)
	if err != nil || version < document.MinimumJava {
		return 0, fmt.Errorf(
			"%q is not a Java major version of %d or later, or auto",
			raw,
			document.MinimumJava,
		)
	}
	return version, nil
}

// promptJavaChange asks for the Java version and distribution that lucy.yaml
// pins. Each answer starts at the current value.
func promptJavaChange(
	ctx context.Context,
	root string,
	gate PromptGate,
	logger *log.Logger,
) (operation.JavaChange, error) {
	d, err := document.ManifestFile(root).Read()
	if err != nil {
		return operation.JavaChange{}, err
	}
	runtimes, err := javaruntime.Installed(ctx, logger)
	if err != nil {
		return operation.JavaChange{}, err
	}
	current := document.ManifestJava{}
	if d.Server.Java != nil {
		current = *d.Server.Java
	}
	version, distribution := current.Version, current.Distribution

	majors := slices.Clone(javaChoices)
	for _, runtime := range runtimes {
		majors = append(majors, runtime.Major)
	}
	if version != 0 {
		majors = append(majors, version)
	}
	slices.Sort(majors)
	majors = slices.Compact(majors)
	slices.Reverse(majors)
	versionOptions := []huh.Option[int]{
		huh.NewOption("auto (the version Mojang recommends)", 0),
	}
	for _, major := range majors {
		versionOptions = append(versionOptions, huh.NewOption("Java "+strconv.Itoa(major), major))
	}
	distributionOptions := []huh.Option[java.Distribution]{
		huh.NewOption("auto (any distribution)", java.DistributionAny),
	}
	for _, known := range java.Distributions {
		distributionOptions = append(
			distributionOptions,
			huh.NewOption(known.Title()+" ("+string(known)+")", known),
		)
	}
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[int]().
				Title("Java version in "+document.ManifestFilename).
				Options(versionOptions...).
				Value(&version),
			huh.NewSelect[java.Distribution]().
				Title("Java distribution in "+document.ManifestFilename).
				Options(distributionOptions...).
				Value(&distribution),
		),
	).
		WithInput(gate.Input()).
		WithOutput(gate.Output())
	if err := NormalizePromptError(ctx, form.RunWithContext(ctx)); err != nil {
		return operation.JavaChange{}, err
	}
	return operation.JavaChange{
		Version:      &version,
		Distribution: &distribution,
	}, nil
}

// lockedJavaRequirement returns the Java requirement of the lock in root, or
// nil when the workspace has no lock.
func lockedJavaRequirement(root string, inv *Invocation) (*javaruntime.Requirement, error) {
	lock, err := document.LockFile(root).Read()
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	requirement := javaruntime.RequirementOf(lock, inv.Logger)
	return &requirement, nil
}

// javaWarnings turns the problems of a Java check into warnings, with a hint
// that names the command that fixes them.
func javaWarnings(check *javaruntime.Check) []error {
	if check == nil || check.Satisfied() {
		return nil
	}
	warnings := make([]error, 0, len(check.Problems)+1)
	for _, problem := range check.Problems {
		warnings = append(warnings, errors.New(problem))
	}
	if check.Source == javaruntime.SourceConfigured {
		warnings = append(warnings, errors.New(
			"run lucy config set java.home <home> to select another Java runtime, "+
				"or lucy config set java.home '' to let Lucy choose",
		))
		return warnings
	}
	warnings = append(warnings, fmt.Errorf(
		"run lucy install --java to install Java %d (%s)",
		check.Target,
		javaDistributionLabel(check.Distribution),
	))
	return warnings
}

func javaDistributionLabel(distribution java.Distribution) string {
	if distribution == java.DistributionAny {
		return "any distribution"
	}
	return distribution.Title()
}

func javaSpecLabel(version string, distribution java.Distribution) string {
	return "Java " + version + " from " + javaDistributionLabel(distribution)
}

func javaOverrideLabel(override *document.ManifestJava) string {
	if override.IsAuto() {
		return javaSpecLabel(document.JavaAuto, java.DistributionAny)
	}
	version := document.JavaAuto
	if override.Version != 0 {
		version = strconv.Itoa(override.Version)
	}
	return javaSpecLabel(version, override.Distribution)
}
