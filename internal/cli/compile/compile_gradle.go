package compile

import (
	"context"
	_ "embed"
	"encoding/json/v2"
	"errors"
	"fmt"
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
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/types"
)

//go:embed compile_model.gradle
var inspectionScript string

type gradleRunner struct {
	dir        string
	executable string
	arguments  []string
	env        []string
	version    string
	modelPath  string
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
) (gradleRunner, error) {
	runner := gradleRunner{dir: build.Dir, version: "unknown"}
	version := ""
	if build.Wrapper != nil {
		wrapper := build.Wrapper
		var scriptMode os.FileMode
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
			if path == wrapper.Script {
				scriptMode = info.Mode()
			}
		}
		if wrapper.DistributionURL == "" {
			return runner, errors.New("Gradle wrapper has no distribution url")
		}
		version = wrapper.Version
		runner.executable = wrapper.Script
		switch {
		case runtime.GOOS == "windows":
			runner.executable = "cmd.exe"
			runner.arguments = []string{"/d", "/c", wrapper.Script}
		case scriptMode.Perm()&0o111 == 0:
			// Repositories sometimes commit the wrapper without the executable
			// bit, so Lucy names the interpreter its shebang asks for.
			runner.executable = wrapperInterpreter(wrapper.Script)
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
	if version != "" {
		runner.version = version
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
		"-Dorg.gradle.java.installations.auto-download=false",
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

// inspect evaluates the build model of every project so Lucy can choose one.
func (r gradleRunner) inspect(ctx context.Context) (gradleModel, error) {
	log.ShowInfo("Evaluating Gradle " + r.version + " build")
	return r.execute(ctx, "Gradle build model", ":lucyCompileModel")
}

// inspectCompilers reports the compilers the selected project can use, which
// decides whether the local JDK can build it. The project's own build task
// joins the invocation as a dry run so the task graph names the compile tasks
// without running any of them: the JDK check must happen before the build.
func (r gradleRunner) inspectCompilers(ctx context.Context, project gradleProject) (gradleProject, error) {
	log.ShowInfo("Checking toolchain for " + nameProject(project.Path))
	model, err := r.execute(
		ctx,
		"Gradle compiler inspection",
		project.BuildTask,
		":lucyCompileModel",
		"-Plucy.compile.project="+project.Path,
		"--dry-run",
	)
	if err != nil {
		return gradleProject{}, err
	}
	for _, candidate := range model.Projects {
		if candidate.Path == project.Path {
			return candidate, nil
		}
	}
	return gradleProject{}, fmt.Errorf("selected project %s disappeared from the build", project.Path)
}

// build runs task on the selected project and returns the archives it produced.
func (r gradleRunner) build(ctx context.Context, task string) (gradleModel, error) {
	log.ShowInfo("Building " + task)
	return r.execute(ctx, "Gradle "+task, task, ":lucyCompileModel")
}

// execute runs label along with tasks. The label names only the work the user
// asked for: tasks also carries Lucy's own inspection task and its -P
// properties, which are implementation detail rather than something to report.
func (r gradleRunner) execute(ctx context.Context, label string, tasks ...string) (gradleModel, error) {
	if err := os.Remove(r.modelPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return gradleModel{}, fmt.Errorf("remove previous Gradle model: %w", err)
	}
	arguments := make([]string, 0, len(r.arguments)+len(tasks))
	arguments = append(arguments, r.arguments...)
	arguments = append(arguments, tasks...)
	output := newProcessLog("gradle")
	cmd := exec.CommandContext(ctx, r.executable, arguments...)
	cmd.Dir = r.dir
	cmd.Env = r.env
	cmd.Stdout = output
	cmd.Stderr = output
	cmd.WaitDelay = 5 * time.Second
	configureProcess(cmd)
	err := cmd.Run()
	output.flush()
	if err != nil {
		if ctx.Err() != nil {
			return gradleModel{}, ctx.Err()
		}
		return gradleModel{}, reportFailure(
			output,
			fmt.Errorf("run %s: %w", label, err),
		)
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

// nameProject names a Gradle project for a status line. The root project
// carries the empty-looking path ":", which reads badly inside a sentence.
func nameProject(path string) string {
	if path == "" || path == ":" {
		return "the root project"
	}
	return path
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
