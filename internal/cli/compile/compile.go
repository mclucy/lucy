package compile

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/mclucy/lucy/internal/buildrepo"
	"github.com/mclucy/lucy/internal/cli"
	"github.com/mclucy/lucy/internal/toolchain"
	"github.com/mclucy/lucy/types"
	"github.com/spf13/cobra"
)

type options struct {
	output   string
	platform string
	project  string
	buildDir string
}

func NewCommand() *cobra.Command {
	var opts options
	cmd := &cobra.Command{
		Use:   "compile <url|owner/repo> -o <artifact.jar>",
		Short: "Compile a Minecraft mod from a Git repository",
		Long: "Clone a Git repository into a temporary directory and compile a Forge, NeoForge, or Fabric mod. " +
			"Compilation executes repository build scripts and can download build dependencies.",
		Args: cobra.ExactArgs(1),
		RunE: cli.WithErrorLogging(func(cmd *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return run(ctx, args[0], opts, cmd.OutOrStdout(), cmd.ErrOrStderr())
		}),
	}
	cmd.Flags().StringVarP(&opts.output, "output", "o", "", "Write the compiled mod JAR to PATH")
	cmd.Flags().StringVar(&opts.platform, cli.FlagPlatform, "", "Select fabric, forge, or neoforge")
	cmd.Flags().StringVar(&opts.project, "project", "", "Select a Gradle project path, such as :fabric")
	cmd.Flags().StringVar(&opts.buildDir, "build-dir", "", "Select a Gradle build directory relative to the checkout")
	if err := cmd.MarkFlagRequired("output"); err != nil {
		panic(err)
	}
	return cmd
}

func run(ctx context.Context, source string, opts options, stdout, stderr io.Writer) (err error) {
	remote, err := parseRemote(source)
	if err != nil {
		return err
	}
	if strings.TrimSpace(opts.output) == "" {
		return errors.New("output path must not be empty")
	}
	if !strings.EqualFold(filepath.Ext(opts.output), ".jar") {
		return errors.New("output path must have a .jar extension")
	}
	if opts.platform != "" && !supportedPlatform(types.Ecosystem(opts.platform)) {
		return errors.New("--platform must be fabric, forge, or neoforge")
	}
	output, err := filepath.Abs(opts.output)
	if err != nil {
		return fmt.Errorf("resolve output path: %w", err)
	}
	if _, err := os.Lstat(output); err == nil {
		return fmt.Errorf("output already exists: %s", output)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect output: %w", err)
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
	if err := clone(ctx, remote, checkout, stderr); err != nil {
		return err
	}
	layout, err := buildrepo.Probe(ctx, checkout)
	if err != nil {
		return fmt.Errorf("probe repository: %w", err)
	}
	build, err := selectBuild(layout, opts.buildDir)
	if err != nil {
		return err
	}
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
	runner, err := newGradleRunner(build, inventory, temporary, stderr)
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
	compilerProject, err := runner.inspectCompilers(ctx, project.Path)
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
	if err := publishArtifact(ctx, artifactPath, output); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(stdout, output); err != nil {
		return fmt.Errorf("report output: %w", err)
	}
	return nil
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
