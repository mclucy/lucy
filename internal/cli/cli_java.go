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
	"github.com/mclucy/lucy/operation"
	"github.com/mclucy/lucy/report"
	"github.com/mclucy/lucy/terminal/style"
	"github.com/spf13/cobra"
)

const (
	flagJavaDistributionName = "distribution"
	flagJavaPathName         = "path"
)

// javaChoices lists the long-term support releases that the interactive form
// offers next to the installed major versions.
var javaChoices = []int{8, 11, 17, 21, 25}

var javaCmd = &cobra.Command{
	Use:   "java <subcommand>",
	Short: help.JavaShort,
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return cmd.Help()
	},
}

var javaListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   help.JavaListShort,
	Args:    cobra.NoArgs,
	RunE:    actionJavaList,
}

var javaUseCmd = &cobra.Command{
	Use:   "use [version|auto]",
	Short: help.JavaUseShort,
	Long:  help.JavaUseLong,
	Args:  cobra.MaximumNArgs(1),
	RunE:  actionJavaUse,
}

var javaListJSON JSONResult[javaListView]

var javaUseJSON JSONResult[*operation.JavaChangeResult]

func NewJavaCommand(contracts *JSONContracts) *cobra.Command {
	javaListJSON.Bind(contracts, javaListCmd)
	javaUseJSON.Bind(contracts, javaUseCmd)
	javaUseCmd.Flags().String(
		flagJavaDistributionName,
		"",
		"Pin a Java distribution in lucy.yaml, or auto to accept any",
	)
	javaUseCmd.Flags().String(
		flagJavaPathName,
		"",
		"Use this Java home or java executable on this machine, or auto to let Lucy choose",
	)
	javaUseCmd.MarkFlagsMutuallyExclusive(flagJavaPathName, flagJavaDistributionName)
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

func actionJavaList(cmd *cobra.Command, _ []string) error {
	root, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("could not determine working directory: %w", err)
	}
	ctx, stop := CommandContext(cmd)
	defer stop()
	requirement, err := lockedJavaRequirement(root)
	if err != nil {
		return err
	}
	runtimes, err := javaruntime.Installed(ctx)
	if err != nil {
		return err
	}
	view := javaListView{Requirement: requirement, Runtimes: []javaRuntimeView{}}
	if requirement != nil {
		if view.Selection, err = javaruntime.Select(ctx, root, *requirement); err != nil {
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
	output := NewQueryOutput(cmd)
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
		writeJavaRequirement(out, requirement)
	}
	if view.Selection != nil && view.Selection.Source == javaruntime.SourceLocal {
		fmt.Fprintf(out, "%s selects %s\n", document.LocalFilename, view.Selection.LocalHome)
	}
	fmt.Fprintln(out)
	if len(view.Runtimes) == 0 {
		fmt.Fprintln(out, "No Java runtime found.")
	}
	for _, entry := range view.Runtimes {
		writeJavaRuntime(out, entry, requirement != nil)
	}
	if view.Selection != nil {
		for _, warning := range javaWarnings(view.Selection) {
			report.Warn(warning)
		}
	}
	return nil
}

func writeJavaRequirement(out io.Writer, requirement *javaruntime.Requirement) {
	target := strconv.Itoa(requirement.Target())
	fmt.Fprintf(
		out,
		"%s %s\n",
		style.Accent("Requires"),
		javaSpecLabel(target, requirement.Distribution),
	)
	width := 0
	for _, c := range requirement.Constraints {
		width = max(width, len(c.Source))
	}
	for _, c := range requirement.Constraints {
		fmt.Fprintf(out, "  %-*s  %s\n", width, c.Source, style.Muted(c.Expression))
	}
}

func writeJavaRuntime(out io.Writer, entry javaRuntimeView, checked bool) {
	marker := " "
	switch {
	case !checked:
	case len(entry.Problems) == 0:
		marker = style.Success("✓")
	default:
		marker = style.Failure("✗")
	}
	line := fmt.Sprintf(
		"%s %-3d %-12s %-22s %s",
		marker,
		entry.Major,
		entry.Version,
		entry.Build(),
		style.Muted(entry.Home),
	)
	if entry.Selected {
		line += " " + style.Accent("(selected)")
	}
	fmt.Fprintln(out, line)
}

func actionJavaUse(cmd *cobra.Command, args []string) error {
	root, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("could not determine working directory: %w", err)
	}
	ctx, stop := CommandContext(cmd)
	defer stop()
	change, err := javaChangeFromFlags(cmd, args)
	if err != nil {
		return err
	}
	if change == (operation.JavaChange{}) {
		if !NewPromptGate(cmd).Available() {
			return fmt.Errorf(
				"specify a Java version or auto, --%s, or --%s; choosing interactively needs a terminal",
				flagJavaDistributionName,
				flagJavaPathName,
			)
		}
		if change, err = promptJavaChange(ctx, root); err != nil {
			return err
		}
	}
	result, err := operation.SetJava(ctx, root, change)
	if err != nil {
		return err
	}
	output := NewOperationOutput(cmd)
	if output.JSON() {
		return javaUseJSON.Write(output, result)
	}
	out := output.Human()
	fmt.Fprintf(out, "%s: %s\n", document.ManifestFilename, javaOverrideLabel(result.Java))
	if result.Home != "" {
		fmt.Fprintf(out, "%s: %s\n", document.LocalFilename, result.Home)
	} else if result.PublishedLocal {
		fmt.Fprintf(out, "%s: Lucy chooses the Java runtime\n", document.LocalFilename)
	}
	requirement, err := lockedJavaRequirement(root)
	if err != nil || requirement == nil {
		return err
	}
	if !result.PublishedLock && result.PublishedManifest {
		fmt.Fprintf(out, "Run lucy install to update %s.\n", document.LockFilename)
		return nil
	}
	check, err := javaruntime.Select(ctx, root, *requirement)
	if err != nil {
		return err
	}
	for _, warning := range javaWarnings(check) {
		report.Warn(warning)
	}
	return nil
}

func javaChangeFromFlags(cmd *cobra.Command, args []string) (operation.JavaChange, error) {
	var change operation.JavaChange
	if len(args) == 1 {
		if cmd.Flags().Changed(flagJavaPathName) {
			return change, fmt.Errorf("a Java version and --%s cannot be used together", flagJavaPathName)
		}
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
	if cmd.Flags().Changed(flagJavaPathName) {
		path, _ := cmd.Flags().GetString(flagJavaPathName)
		path = strings.TrimSpace(path)
		if path == "" {
			return change, fmt.Errorf("--%s needs a Java home, a java executable, or auto", flagJavaPathName)
		}
		if path == document.JavaAuto {
			path = ""
		}
		change.Path = &path
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
func promptJavaChange(ctx context.Context, root string) (operation.JavaChange, error) {
	d, err := document.ManifestFile(root).Read()
	if err != nil {
		return operation.JavaChange{}, err
	}
	runtimes, err := javaruntime.Installed(ctx)
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
	)
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
func lockedJavaRequirement(root string) (*javaruntime.Requirement, error) {
	lock, err := document.LockFile(root).Read()
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	requirement := javaruntime.RequirementOf(lock)
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
	if check.Source == javaruntime.SourceLocal {
		warnings = append(warnings, fmt.Errorf(
			"run lucy java use --%s <home> to select another Java runtime, or --%s auto to let Lucy choose",
			flagJavaPathName,
			flagJavaPathName,
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
