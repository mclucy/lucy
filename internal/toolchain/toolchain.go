package toolchain

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

const probeTimeout = 20 * time.Second

type JavaInstallation struct {
	Home    string
	Java    string
	Javac   string
	Version string
	Major   int
	Vendor  string
	Arch    string
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

// Discover probes installed executables without provisioning tools. Missing tools
// and unusable candidates are inventory results; additional homes are local JDK hints.
func Discover(ctx context.Context, homes ...string) (Inventory, error) {
	if err := ctx.Err(); err != nil {
		return Inventory{}, err
	}
	environment, err := readEnv()
	if err != nil {
		return Inventory{}, err
	}
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
			results[i], problems[i] = probeJava(ctx, candidate)
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
	slices.SortFunc(inventory.Java, func(a, b JavaInstallation) int {
		return cmp.Or(cmp.Compare(b.Major, a.Major), cmp.Compare(a.Home, b.Home))
	})
	inventory.Gradle, err = probeGradle(ctx)
	if err != nil {
		inventory.Problems = append(inventory.Problems, err)
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
