package compile

import (
	"context"
	_ "embed"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/mclucy/lucy/internal/buildrepo"
	"github.com/mclucy/lucy/internal/toolchain"
	"github.com/mclucy/lucy/types"
)

//go:embed compile_model.gradle
var inspectionScript string

type gradleRunner struct {
	dir        string
	executable string
	arguments  []string
	env        []string
	modelPath  string
	stderr     io.Writer
	daemon     toolchain.JavaInstallation
}

type gradleModel struct {
	Projects []gradleProject `json:"projects"`
}

type gradleProject struct {
	Path string `json:"path"`

	Ecosystems     []types.Ecosystem `json:"ecosystems"`
	HasModMetadata bool              `json:"hasModMetadata"`
	BuildTask      string            `json:"buildTask"`
	Compilers      []compiler        `json:"compilers"`
	Archives       []archive         `json:"archives"`
}

type compiler struct {
	Task       string `json:"task"`
	Major      int    `json:"major"`
	Executable string `json:"executable"`
	Release    int    `json:"release"`
	Target     string `json:"target"`
}

type archive struct {
	File       string `json:"file"`
	Classifier string `json:"classifier"`
	Published  bool   `json:"published"`
	Enabled    bool   `json:"enabled"`
}

func newGradleRunner(
	build buildrepo.GradleBuild,
	inventory toolchain.Inventory,
	temporary string,
	stderr io.Writer,
) (gradleRunner, error) {
	runner := gradleRunner{dir: build.Dir, stderr: stderr}
	version := ""
	if build.Wrapper != nil {
		wrapper := build.Wrapper
		for _, path := range []string{wrapper.Script, wrapper.Jar, wrapper.Properties} {
			if path == "" {
				return runner, errors.New("repository Gradle wrapper is incomplete")
			}
			info, err := os.Stat(path)
			if err != nil {
				return runner, fmt.Errorf("inspect Gradle wrapper component %q: %w", path, err)
			}
			if !info.Mode().IsRegular() {
				return runner, fmt.Errorf("Gradle wrapper component is not a regular file: %s", path)
			}
		}
		if wrapper.DistributionURL == "" {
			return runner, errors.New("Gradle wrapper has no distribution url")
		}
		version = wrapper.Version
		runner.executable = wrapper.Script
		if runtime.GOOS == "windows" {
			runner.executable = "cmd.exe"
			runner.arguments = []string{"/d", "/c", wrapper.Script}
		} else {
			runner.executable = "/bin/sh"
			runner.arguments = []string{wrapper.Script}
		}
	} else if inventory.Gradle != nil {
		runner.executable = inventory.Gradle.Path
		version = inventory.Gradle.Version
		extension := strings.ToLower(filepath.Ext(runner.executable))
		if runtime.GOOS == "windows" && (extension == ".bat" || extension == ".cmd") {
			runner.arguments = []string{"/d", "/c", runner.executable}
			runner.executable = "cmd.exe"
		}
	} else {
		return runner, errors.Join(
			errors.New("repository has no Gradle wrapper and no local Gradle installation was found"),
			errors.Join(inventory.Problems...),
		)
	}
	launcher, daemon, err := selectJVMs(build, inventory, version)
	if err != nil {
		return runner, errors.Join(err, errors.Join(inventory.Problems...))
	}
	runner.daemon = daemon
	initPath := filepath.Join(temporary, "lucy-init.gradle")
	if err := os.WriteFile(initPath, []byte(inspectionScript), 0o600); err != nil {
		return runner, fmt.Errorf("write Gradle inspection script: %w", err)
	}
	runner.modelPath = filepath.Join(temporary, "gradle-model.json")
	runner.arguments = append(runner.arguments,
		"--no-daemon", "--console=plain", "--no-configure-on-demand", "--init-script", initPath,
		"-Porg.gradle.java.installations.auto-download=false",
	)
	if parsed, err := semver.NewVersion(version); err != nil || parsed.GreaterThanEqual(semver.MustParse("6.6.0")) {
		runner.arguments = append(runner.arguments, "--no-configuration-cache")
	}
	if build.Daemon == nil {
		runner.arguments = append(runner.arguments, "-Dorg.gradle.java.home="+daemon.Home)
	}
	homes := make([]string, 0, len(inventory.Java))
	for _, java := range inventory.Java {
		if java.Javac != "" {
			homes = append(homes, java.Home)
		}
	}
	runner.env = replaceEnvironment(os.Environ(), "JAVA_HOME", launcher.Home)
	installationPathsKey := "ORG_GRADLE_PROJECT_org.gradle.java.installations.paths"
	if _, configured := os.LookupEnv(installationPathsKey); !configured && len(homes) > 0 {
		runner.env = replaceEnvironment(runner.env, installationPathsKey, strings.Join(homes, ","))
	}
	runner.env = replaceEnvironment(runner.env, "LUCY_COMPILE_MODEL", runner.modelPath)
	runner.env = replaceEnvironment(runner.env, "PATH", filepath.Dir(launcher.Java)+string(os.PathListSeparator)+os.Getenv("PATH"))
	return runner, nil
}

func (r gradleRunner) inspect(ctx context.Context) (gradleModel, error) {
	return r.execute(ctx, ":lucyCompileModel")
}

func (r gradleRunner) inspectCompilers(ctx context.Context, project string) (gradleProject, error) {
	model, err := r.execute(ctx, ":lucyCompileModel", "-Plucy.compile.project="+project)
	if err != nil {
		return gradleProject{}, err
	}
	for _, candidate := range model.Projects {
		if candidate.Path == project {
			return candidate, nil
		}
	}
	return gradleProject{}, fmt.Errorf("selected project %s disappeared from the build", project)
}

func (r gradleRunner) build(ctx context.Context, task string) (gradleModel, error) {
	return r.execute(ctx, task, ":lucyCompileModel", "-Plucy.compile.task="+task)
}

func (r gradleRunner) execute(ctx context.Context, tasks ...string) (gradleModel, error) {
	if err := os.Remove(r.modelPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return gradleModel{}, fmt.Errorf("remove previous Gradle model: %w", err)
	}
	arguments := make([]string, 0, len(r.arguments)+len(tasks))
	arguments = append(arguments, r.arguments...)
	arguments = append(arguments, tasks...)
	cmd := exec.CommandContext(ctx, r.executable, arguments...)
	cmd.Dir = r.dir
	cmd.Env = r.env
	cmd.Stdout = r.stderr
	cmd.Stderr = r.stderr
	cmd.WaitDelay = 5 * time.Second
	configureProcess(cmd)
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return gradleModel{}, ctx.Err()
		}
		return gradleModel{}, fmt.Errorf("execute Gradle %s: %w", strings.Join(tasks, " "), err)
	}
	data, err := os.ReadFile(r.modelPath)
	if err != nil {
		return gradleModel{}, fmt.Errorf("read evaluated Gradle model: %w", err)
	}
	var model gradleModel
	if err := json.Unmarshal(data, &model); err != nil {
		return model, fmt.Errorf("decode evaluated Gradle model: %w", err)
	}
	return model, nil
}

func selectProject(model gradleModel, opts options) (gradleProject, error) {
	candidates := make([]gradleProject, 0, len(model.Projects))
	platform := types.Ecosystem(opts.platform)
	for _, project := range model.Projects {
		if opts.project != "" && project.Path != opts.project {
			continue
		}
		if platform != "" && !slices.Contains(project.Ecosystems, platform) {
			continue
		}
		hasModArchive := slices.ContainsFunc(project.Archives, func(output archive) bool {
			return output.Enabled && filepath.Ext(output.File) == ".jar" && !developmentClassifier(output.Classifier)
		})
		if hasModArchive && slices.ContainsFunc(project.Ecosystems, supportedPlatform) {
			candidates = append(candidates, project)
		}
	}
	if slices.ContainsFunc(candidates, func(project gradleProject) bool { return project.HasModMetadata }) {
		candidates = slices.DeleteFunc(candidates, func(project gradleProject) bool { return !project.HasModMetadata })
	}
	if len(candidates) == 0 {
		return gradleProject{}, errors.New("no matching Forge, NeoForge, or Fabric mod project was found")
	}
	if len(candidates) != 1 {
		paths := make([]string, 0, len(candidates))
		for _, project := range candidates {
			paths = append(paths, project.Path+" ["+joinEcosystems(project.Ecosystems)+"]")
		}
		return gradleProject{}, fmt.Errorf("multiple mod projects found; select --platform or --project: %s", strings.Join(paths, ", "))
	}
	project := candidates[0]
	if project.BuildTask == "" {
		return project, fmt.Errorf("project %s has neither a build nor an assemble task", project.Path)
	}
	return project, nil
}

func joinEcosystems(ecosystems []types.Ecosystem) string {
	values := make([]string, 0, len(ecosystems))
	for _, ecosystem := range ecosystems {
		values = append(values, string(ecosystem))
	}
	return strings.Join(values, ", ")
}

func replaceEnvironment(environment []string, key, value string) []string {
	for i, entry := range environment {
		name, _, _ := strings.Cut(entry, "=")
		if strings.EqualFold(name, key) {
			environment[i] = key + "=" + value
			return environment
		}
	}
	return append(environment, key+"="+value)
}
