package toolchain

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	javapkg "github.com/mclucy/lucy/java"
)

const probeTimeout = 20 * time.Second

type JavaInstallation struct {
	Home         string
	Java         string
	Javac        string
	Version      string
	Major        int
	Vendor       string
	Arch         string
	Distribution javapkg.Distribution
	Implementor  string
}

type GradleInstallation struct {
	Path    string
	Version string
}

type Inventory struct {
	Java           []JavaInstallation
	Gradle         *GradleInstallation
	JavaHome       string
	PathJava       string
	GradleUserHome string
	Problems       []error
}

// DiscoverJava probes installed Java runtimes without provisioning tools and
// without running their compilers. Missing tools and unusable candidates are
// inventory results; additional homes are local JDK hints.
func DiscoverJava(ctx context.Context, homes ...string) (Inventory, error) {
	if err := ctx.Err(); err != nil {
		return Inventory{}, err
	}
	environment, err := readEnv()
	if err != nil {
		return Inventory{}, err
	}
	return discover(ctx, environment, homes, false)
}

// DiscoverBuildTools probes installed Java runtimes, their compilers, and
// Gradle. Build commands use it; runtime selection uses DiscoverJava.
func DiscoverBuildTools(ctx context.Context, homes ...string) (Inventory, error) {
	if err := ctx.Err(); err != nil {
		return Inventory{}, err
	}
	environment, err := readEnv()
	if err != nil {
		return Inventory{}, err
	}
	return discover(ctx, environment, homes, true)
}

func discover(ctx context.Context, environment envVars, homes []string, compilers bool) (Inventory, error) {
	inventory := Inventory{
		JavaHome: environment.javaHome, PathJava: environment.pathJava,
		GradleUserHome: environment.gradleUserHome,
	}
	candidates := javaCandidates(environment, homes)
	results := make([]JavaInstallation, len(candidates))
	problems := make([]error, len(candidates))
	concurrency := make(chan struct{}, 4)
	var workers sync.WaitGroup
	for i, candidate := range candidates {
		workers.Go(func() {
			select {
			case concurrency <- struct{}{}:
				defer func() { <-concurrency }()
			case <-ctx.Done():
				return
			}
			results[i], problems[i] = probeJava(ctx, candidate, compilers)
		})
	}
	workers.Wait()
	if err := ctx.Err(); err != nil {
		return Inventory{}, err
	}
	seen := make(map[string]bool, len(results))
	for i, java := range results {
		if problems[i] != nil {
			inventory.Problems = append(inventory.Problems, problems[i])
			continue
		}
		if java.Home != "" && !seen[java.Home] {
			seen[java.Home] = true
			inventory.Java = append(inventory.Java, java)
		}
	}
	sort.Slice(inventory.Java, func(i, j int) bool {
		a, b := &inventory.Java[i], &inventory.Java[j]
		return cmp.Or(cmp.Compare(b.Major, a.Major), cmp.Compare(a.Home, b.Home)) < 0
	})
	if compilers {
		var err error
		inventory.Gradle, err = probeGradle(ctx)
		if err != nil {
			inventory.Problems = append(inventory.Problems, err)
		}
	}
	if err := ctx.Err(); err != nil {
		return Inventory{}, err
	}
	return inventory, nil
}

type envVars struct {
	javaHome       string
	pathJava       string
	gradleUserHome string
	home           string
	path           string
}

func readEnv() (envVars, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return envVars{}, fmt.Errorf("find user home: %w", err)
	}
	environment := envVars{
		javaHome: os.Getenv("JAVA_HOME"), gradleUserHome: os.Getenv("GRADLE_USER_HOME"),
		home: home, path: os.Getenv("PATH"),
	}
	if environment.gradleUserHome == "" {
		environment.gradleUserHome = filepath.Join(home, ".gradle")
	}
	if java, err := exec.LookPath(javaExecutableName); err == nil {
		environment.pathJava = java
	}
	return environment, nil
}

func canonical(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(path)
}

func probeEnv() []string {
	environment := os.Environ()
	return slices.DeleteFunc(environment, func(entry string) bool {
		key, _, _ := strings.Cut(entry, "=")
		switch strings.ToUpper(key) {
		case "_JAVA_OPTIONS", "JAVA_TOOL_OPTIONS", "JDK_JAVA_OPTIONS":
			return true
		default:
			return false
		}
	})
}
