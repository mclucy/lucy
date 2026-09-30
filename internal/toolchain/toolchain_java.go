package toolchain

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type javaCandidate struct {
	java   string
	source string
}

func javaCandidates(environment envVars, hints []string) []javaCandidate {
	candidates := make([]javaCandidate, 0, len(hints)+2)
	seen := make(map[string]bool)
	add := func(executable, source string) {
		if executable == "" || managerShim(executable) {
			return
		}
		key := canonical(executable)
		if !seen[key] {
			seen[key] = true
			candidates = append(candidates, javaCandidate{java: executable, source: source})
		}
	}
	if environment.javaHome != "" {
		add(filepath.Join(environment.javaHome, "bin", javaExecutableName), "JAVA_HOME")
	}
	add(environment.pathJava, "PATH")
	for _, home := range hints {
		if home != "" {
			add(filepath.Join(home, "bin", javaExecutableName), "Gradle configuration")
		}
	}
	roots := systemJavaRoots()
	roots = append(roots,
		filepath.Join(environment.home, ".sdkman", "candidates", "java"),
		filepath.Join(environment.home, ".asdf", "installs", "java"),
		filepath.Join(environment.home, ".local", "share", "mise", "installs", "java"),
		filepath.Join(environment.home, ".jabba", "jdk"),
		filepath.Join(environment.gradleUserHome, "jdks"),
	)
	for _, root := range roots {
		for _, home := range javaHomesUnder(root) {
			add(filepath.Join(home, "bin", javaExecutableName), root)
		}
	}
	return candidates
}

func javaHomesUnder(root string) []string {
	homes := make([]string, 0)
	add := func(dir string) bool {
		for _, suffix := range []string{"", filepath.Join("Contents", "Home")} {
			home := filepath.Join(dir, suffix)
			if isExecutable(filepath.Join(home, "bin", javaExecutableName)) {
				homes = append(homes, home)
				return true
			}
		}
		return false
	}
	if add(root) {
		return homes
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return homes
	}
	for _, entry := range entries {
		child := filepath.Join(root, entry.Name())
		if add(child) {
			continue
		}
		if filepath.Base(root) != "jdks" {
			continue
		}
		nested, err := os.ReadDir(child)
		if err != nil {
			continue
		}
		for _, entry := range nested {
			add(filepath.Join(child, entry.Name()))
		}
	}
	return homes
}

func probeJava(ctx context.Context, candidate javaCandidate) (JavaInstallation, error) {
	if !isExecutable(candidate.java) {
		return JavaInstallation{}, fmt.Errorf("java candidate %q from %s is not executable", candidate.java, candidate.source)
	}
	output, err := runProbe(ctx, candidate.java, "-XshowSettings:properties", "-version")
	if err != nil {
		return JavaInstallation{}, fmt.Errorf("probe java %q: %w", candidate.java, err)
	}
	properties := make(map[string]string)
	for line := range strings.SplitSeq(output, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), " = ")
		if ok {
			properties[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	if properties["java.home"] == "" {
		return JavaInstallation{}, fmt.Errorf("java %q reported no installation home", candidate.java)
	}
	home := canonical(properties["java.home"])
	if filepath.Base(home) == "jre" {
		parent := filepath.Dir(home)
		if isExecutable(filepath.Join(parent, "bin", javacExecutableName)) {
			home = parent
		}
	}
	installation := JavaInstallation{
		Home: home, Java: filepath.Join(home, "bin", javaExecutableName),
		Version: properties["java.version"], Major: majorVersion(properties["java.version"]),
		Vendor: properties["java.vendor"], Arch: properties["os.arch"],
	}
	if installation.Major == 0 || !isExecutable(installation.Java) {
		return JavaInstallation{}, fmt.Errorf("java %q reported an unusable installation", candidate.java)
	}
	javac := filepath.Join(home, "bin", javacExecutableName)
	if isExecutable(javac) {
		if _, err := runProbe(ctx, javac, "-version"); err == nil {
			installation.Javac = javac
		}
	}
	return installation, nil
}

func majorVersion(version string) int {
	version = strings.TrimPrefix(version, "1.")
	end := strings.IndexFunc(version, func(r rune) bool { return r < '0' || r > '9' })
	if end >= 0 {
		version = version[:end]
	}
	major, err := strconv.Atoi(version)
	if err != nil {
		return 0
	}
	return major
}

func runProbe(ctx context.Context, executable string, arguments ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	cmd := probeCommand(ctx, executable, arguments...)
	cmd.Env = probeEnv()
	cmd.WaitDelay = time.Second
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	return string(output), err
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	if runtime.GOOS == "windows" {
		switch strings.ToLower(filepath.Ext(path)) {
		case ".exe", ".bat", ".cmd":
			return true
		default:
			return false
		}
	}
	return info.Mode().Perm()&0o111 != 0
}

func managerShim(path string) bool {
	normalized := "/" + strings.ToLower(filepath.ToSlash(path)) + "/"
	return strings.Contains(normalized, "/shims/")
}
