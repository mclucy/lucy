package compile

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/internal/buildrepo"
	"github.com/mclucy/lucy/internal/cli"
	"github.com/mclucy/lucy/internal/cli/help"
	"github.com/mclucy/lucy/internal/toolchain"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/report"
	"github.com/spf13/cobra"
)

type options struct {
	output    string
	ecosystem string
	project   string
	buildDir  string
	branch    string
	tag       string
}

func NewCommand() *cobra.Command {
	var opts options
	cmd := &cobra.Command{
		Use:   "compile <url|owner/repo>",
		Short: help.CompileShort,
		Long:  help.CompileLong,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cli.JSONMode(cmd) != cli.JSONNone {
				return &report.Detail{
					Code:  "usage.unsupported_output",
					Title: "compile does not support JSON output",
					Hint:  "Run without --json or --json-compact.",
				}
			}
			ctx, stop := cli.CommandContext(cmd)
			defer stop()
			return run(
				ctx,
				args[0],
				&opts,
				cli.NewPromptGate(cmd),
				cmd.OutOrStdout(),
				cmd.ErrOrStderr(),
			)
		},
	}
	cmd.Flags().StringVarP(&opts.output, "output", "o", "", "Write the compiled mod JAR to PATH, or to a directory under the project's artifact name")
	cmd.Flags().StringVar(&opts.ecosystem, cli.FlagEcosystem, "", "Select fabric, forge, or neoforge")
	cmd.Flags().StringVar(&opts.project, "project", "", "Select a Gradle project path, such as :fabric")
	cmd.Flags().StringVar(&opts.buildDir, "build-dir", "", "Select a Gradle build directory relative to the checkout")
	cmd.Flags().StringVar(&opts.branch, "branch", "", "Checkout Git branch BRANCH before compiling")
	cmd.Flags().StringVar(&opts.tag, "tag", "", "Checkout Git tag TAG before compiling")
	return cmd
}

// run compiles source and writes the resulting JAR. Progress is narrated on
// stderr; stdout carries only the artifact path so the command stays pipeable.
func run(ctx context.Context, source string, opts *options, gate cli.PromptGate, stdout, stderr io.Writer) (err error) {
	remote, err := parseRemote(source)
	if err != nil {
		return err
	}
	destination := parseOutputDestination(opts.output)
	if opts.ecosystem != "" && !supportedEcosystem(catalog.Ecosystem(opts.ecosystem)) {
		return errors.New("--ecosystem must be fabric, forge, or neoforge")
	}
	branch := strings.TrimSpace(opts.branch)
	tag := strings.TrimSpace(opts.tag)
	if branch != "" && tag != "" {
		return errors.New("--branch and --tag are mutually exclusive")
	}
	if strings.HasPrefix(branch, "-") || strings.HasPrefix(tag, "-") {
		return errors.New("branch and tag names must not start with '-'")
	}
	output := ""
	if destination.file != "" {
		if output, err = destination.resolve(""); err != nil {
			return err
		}
		if err := ensureAbsent(output); err != nil {
			return err
		}
	}

	temporary, err := os.MkdirTemp("", "lucy-compile-*")
	if err != nil {
		return fmt.Errorf("create temporary checkout: %w", err)
	}
	defer func() {
		if cleanupErr := os.RemoveAll(temporary); cleanupErr != nil {
			err = errors.Join(err, fmt.Errorf("remove temporary checkout: %w", cleanupErr))
		}
	}()
	checkout := filepath.Join(temporary, "repository")
	ref := refSuffix(branch, tag)
	log.Info("compilation checkout started", "branch", branch, "tag", tag)
	writeProgress(stderr, "Cloning "+source+ref)
	if err := clone(ctx, remote, checkout, branch, tag, newProcessLog("git")); err != nil {
		return err
	}
	log.Info("compilation checkout completed", "directory", checkout)
	writeProgress(stderr, "Cloned "+source+ref)

	log.Info("compilation repository inspection started", "directory", checkout)
	layout, err := buildrepo.Probe(ctx, checkout)
	if err != nil {
		return fmt.Errorf("probe repository: %w", err)
	}
	reportFindings(layout.Root, layout.Findings)
	build, err := selectBuild(layout, opts.buildDir)
	if err != nil {
		return err
	}
	log.Debug("Gradle build selected", "directory", build.Dir, "requested", opts.buildDir,
		"reason", "requested directory or root/unique Gradle build")
	log.Info("compilation toolchain preparation started", "directory", build.Dir)

	writeProgress(stderr, "Preparing the Gradle build")
	gradleUserHome := os.Getenv("GRADLE_USER_HOME")
	if gradleUserHome == "" {
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			return fmt.Errorf("find user home: %w", homeErr)
		}
		gradleUserHome = filepath.Join(home, ".gradle")
	}
	javaHome, err := configuredGradleJavaHome(build, gradleUserHome)
	if err != nil {
		return err
	}
	build.JavaHome = javaHome
	inventory, err := toolchain.Discover(ctx, javaHome)
	if err != nil {
		return fmt.Errorf("discover local toolchains: %w", err)
	}
	for _, warning := range toolchainWarnings(build, inventory) {
		warnErr := errors.New(warning)
		log.Warn(warnErr)
		report.Warn(warnErr)
	}
	runner, err := newGradleRunner(build, inventory, temporary, stderr)
	if err != nil {
		return err
	}
	model, err := runner.inspect(ctx)
	if err != nil {
		return err
	}
	project, err := selectProject(model, opts, gate)
	if err != nil {
		return err
	}
	log.Debug("compilation project selected", "project", project.Path, "task", project.BuildTask,
		"reason", "project selection and supported mod artifact metadata")
	// selectProject runs again on the rebuilt model; pin the pick so the
	// second pass keeps the same project instead of listing candidates anew.
	opts.project = project.Path
	compilerProject, err := runner.inspectCompilers(ctx, project)
	if err != nil {
		return err
	}
	if err := checkCompiler(compilerProject, runner.daemon); err != nil {
		return err
	}
	model, err = runner.build(ctx, project.BuildTask)
	if err != nil {
		return err
	}
	project, err = selectProject(model, opts, gate)
	if err != nil {
		return err
	}
	artifactPath, err := selectArtifact(project, catalog.Ecosystem(opts.ecosystem))
	if err != nil {
		return err
	}
	if output == "" {
		if output, err = destination.resolve(filepath.Base(artifactPath)); err != nil {
			return err
		}
		if err := ensureAbsent(output); err != nil {
			return err
		}
	}
	if err := publishArtifact(ctx, artifactPath, output); err != nil {
		return err
	}
	log.Info("compiled artifact published", "path", output, "project", project.Path)
	writeProgress(stderr, "Compiled "+filepath.Base(output))
	if _, err := fmt.Fprintln(stdout, output); err != nil {
		return fmt.Errorf("report output: %w", err)
	}
	return nil
}

// writeProgress narrates compilation progress on stderr. Progress text is
// advisory, so a write failure does not stop the build.
func writeProgress(writer io.Writer, line string) {
	_, _ = fmt.Fprintln(writer, line)
}

// reportFindings surfaces what the static probe could not settle on its own.
// None of them stops a build, but each marks a decision left to Gradle, and
// silently discarding one would leave the user with a wrong model of what Lucy
// verified before running an untrusted build.
func reportFindings(root string, findings []buildrepo.Finding) {
	for _, finding := range findings {
		location := finding.File
		if relative, err := filepath.Rel(root, finding.File); err == nil {
			location = filepath.ToSlash(relative)
		}
		if finding.Line > 0 {
			location += ":" + strconv.Itoa(finding.Line)
		}
		warning := fmt.Errorf("%s: %s", location, finding.Message)
		log.Warn(warning)
		report.Warn(warning)
	}
}

func supportedEcosystem(ecosystem catalog.Ecosystem) bool {
	switch ecosystem {
	case catalog.EcosystemFabric, catalog.EcosystemForge, catalog.EcosystemNeoForge:
		return true
	default:
		return false
	}
}

func selectBuild(layout buildrepo.Layout, requested string) (buildrepo.GradleBuild, error) {
	if requested != "" {
		if !filepath.IsLocal(requested) {
			return buildrepo.GradleBuild{}, errors.New("--build-dir must remain inside the checkout")
		}
		dir := filepath.Join(layout.Root, requested)
		for _, build := range layout.Builds {
			if build.Dir == dir {
				return build, nil
			}
		}
		return buildrepo.GradleBuild{}, fmt.Errorf("no Gradle build at %q", requested)
	}
	for _, build := range layout.Builds {
		if build.Dir == layout.Root {
			return build, nil
		}
	}
	included := make(map[string]bool)
	for _, build := range layout.Builds {
		for _, dir := range build.IncludedBuilds {
			included[dir] = true
		}
	}
	candidates := make([]buildrepo.GradleBuild, 0, len(layout.Builds))
	for _, build := range layout.Builds {
		base := filepath.Base(build.Dir)
		isBuildLogic := base == "buildSrc" || base == "build-logic" || base == "buildlogic"
		if !included[build.Dir] && !isBuildLogic {
			candidates = append(candidates, build)
		}
	}
	if len(candidates) == 1 {
		return candidates[0], nil
	}
	if len(candidates) == 0 {
		return buildrepo.GradleBuild{}, errors.New("repository contains no Gradle build")
	}
	paths := make([]string, 0, len(candidates))
	for _, build := range candidates {
		relative, err := filepath.Rel(layout.Root, build.Dir)
		if err != nil {
			return buildrepo.GradleBuild{}, fmt.Errorf("describe build directory: %w", err)
		}
		paths = append(paths, relative)
	}
	return buildrepo.GradleBuild{}, fmt.Errorf("multiple Gradle builds found; select --build-dir: %s", strings.Join(paths, ", "))
}
