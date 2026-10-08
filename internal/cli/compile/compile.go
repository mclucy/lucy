package compile

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/mclucy/lucy/internal/buildrepo"
	"github.com/mclucy/lucy/internal/cli"
	"github.com/mclucy/lucy/internal/toolchain"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/types"
	"github.com/spf13/cobra"
)

type options struct {
	output   string
	platform string
	project  string
	buildDir string
	branch   string
	tag      string
}

func NewCommand() *cobra.Command {
	var opts options
	cmd := &cobra.Command{
		Use:   "compile <url|owner/repo> [-o <artifact.jar|directory>]",
		Short: "Compile a Minecraft mod from a Git repository",
		Long: "Clone a Git repository into a temporary directory and compile a Forge, NeoForge, or Fabric mod. " +
			"Compilation executes repository build scripts and can download build dependencies.",
		Args: cobra.ExactArgs(1),
		RunE: cli.WithErrorLogging(func(cmd *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return run(ctx, args[0], opts, cmd.OutOrStdout())
		}),
	}
	cmd.Flags().StringVarP(&opts.output, "output", "o", "", "Write the compiled mod JAR to PATH, or to a directory under the project's artifact name")
	cmd.Flags().StringVar(&opts.platform, cli.FlagPlatform, "", "Select fabric, forge, or neoforge")
	cmd.Flags().StringVar(&opts.project, "project", "", "Select a Gradle project path, such as :fabric")
	cmd.Flags().StringVar(&opts.buildDir, "build-dir", "", "Select a Gradle build directory relative to the checkout")
	cmd.Flags().StringVar(&opts.branch, "branch", "", "Checkout Git branch BRANCH before compiling")
	cmd.Flags().StringVar(&opts.tag, "tag", "", "Checkout Git tag TAG before compiling")
	return cmd
}

// run compiles source and writes the resulting JAR. Progress is narrated on
// stderr; stdout carries only the artifact path so the command stays pipeable.
func run(ctx context.Context, source string, opts options, stdout io.Writer) (err error) {
	remote, err := parseRemote(source)
	if err != nil {
		return err
	}
	destination := parseOutputDestination(opts.output)
	if opts.platform != "" && !supportedPlatform(types.Ecosystem(opts.platform)) {
		return errors.New("--platform must be fabric, forge, or neoforge")
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
	log.ShowInfo("Cloning " + source + ref)
	if err := clone(ctx, remote, checkout, branch, tag, newProcessLog("git")); err != nil {
		return err
	}
	log.ShowInfo("Cloned " + source + ref)

	layout, err := buildrepo.Probe(ctx, checkout)
	if err != nil {
		return fmt.Errorf("probe repository: %w", err)
	}
	reportFindings(layout.Root, layout.Findings)
	build, err := selectBuild(layout, opts.buildDir)
	if err != nil {
		return err
	}

	log.ShowInfo("Preparing the Gradle build")
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
		log.ShowWarn(errors.New(warning))
	}
	runner, err := newGradleRunner(build, inventory, temporary)
	if err != nil {
		return err
	}
	model, err := runner.inspect(ctx)
	if err != nil {
		return err
	}
	project, err := selectProject(model, opts)
	if err != nil {
		return err
	}
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
	project, err = selectProject(model, opts)
	if err != nil {
		return err
	}
	artifactPath, err := selectArtifact(project, types.Ecosystem(opts.platform))
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
	log.ShowInfo("Compiled " + filepath.Base(output))
	if _, err := fmt.Fprintln(stdout, output); err != nil {
		return fmt.Errorf("report output: %w", err)
	}
	return nil
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
		log.ShowWarn(fmt.Errorf("%s: %s", location, finding.Message))
	}
}

func supportedPlatform(platform types.Ecosystem) bool {
	switch platform {
	case types.EcoFabric, types.EcoForge, types.EcoNeoforge:
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
