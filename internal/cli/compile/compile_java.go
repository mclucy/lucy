package compile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/mclucy/lucy/internal/buildrepo"
	"github.com/mclucy/lucy/internal/toolchain"
)

func selectJVMs(
	build buildrepo.GradleBuild,
	inventory toolchain.Inventory,
	gradleVersion string,
) (toolchain.JavaInstallation, toolchain.JavaInstallation, error) {
	var gradle *semver.Version
	if parsed, err := semver.NewVersion(gradleVersion); err == nil {
		gradle = parsed
	}
	var launcher toolchain.JavaInstallation
	if configured := os.Getenv("JAVA_HOME"); configured != "" {
		java, err := javaAtHome(inventory, configured)
		if err != nil {
			return launcher, launcher, fmt.Errorf("configured JAVA_HOME: %w", err)
		}
		if gradleSupportsJava(gradle, java.Major) {
			launcher = java
		}
	}
	if launcher.Home == "" {
		for _, java := range inventory.Java {
			if gradleSupportsJava(gradle, java.Major) {
				launcher = java
				break
			}
		}
	}
	if launcher.Home == "" {
		return launcher, launcher, fmt.Errorf("no local Java installation can launch Gradle %s", gradleVersion)
	}
	daemon := launcher
	if build.Daemon != nil {
		var found bool
		for _, java := range inventory.Java {
			versionMatches := build.Daemon.Major == 0 || java.Major == build.Daemon.Major
			if versionMatches && vendorMatches(build.Daemon.Vendor, java.Vendor) && gradleSupportsJava(gradle, java.Major) {
				daemon = java
				found = true
				break
			}
		}
		if !found {
			description := fmt.Sprintf("Gradle daemon requires local Java %d", build.Daemon.Major)
			if build.Daemon.Vendor != "" {
				description += " from " + build.Daemon.Vendor
			}
			return launcher, daemon, errors.New(description)
		}
	} else if build.JavaHome != "" {
		var err error
		daemon, err = javaAtHome(inventory, build.JavaHome)
		if err != nil {
			return launcher, daemon, fmt.Errorf("configured Gradle java home: %w", err)
		}
	}
	if !gradleSupportsJava(gradle, daemon.Major) {
		return launcher, daemon, fmt.Errorf("Gradle %s cannot run its daemon on Java %d", gradleVersion, daemon.Major)
	}
	return launcher, daemon, nil
}

func javaAtHome(inventory toolchain.Inventory, home string) (toolchain.JavaInstallation, error) {
	resolved, err := filepath.EvalSymlinks(home)
	if err != nil {
		return toolchain.JavaInstallation{}, fmt.Errorf("resolve Java home %q: %w", home, err)
	}
	for _, java := range inventory.Java {
		if java.Home == filepath.Clean(resolved) {
			return java, nil
		}
	}
	return toolchain.JavaInstallation{}, fmt.Errorf("no usable Java installation at %q", home)
}

func configuredGradleJavaHome(build buildrepo.GradleBuild, userHome string) (string, error) {
	home := build.JavaHome
	if userHome == "" {
		return home, nil
	}
	path := filepath.Join(userHome, "gradle.properties")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return home, nil
	}
	if err != nil {
		return "", fmt.Errorf("read user Gradle properties: %w", err)
	}
	properties, err := buildrepo.ParseProperties(string(data))
	if err != nil {
		return "", fmt.Errorf("decode user Gradle properties: %w", err)
	}
	if configured, exists := properties["org.gradle.java.home"]; exists {
		home = configured
	}
	if home != "" && !filepath.IsAbs(home) {
		home = filepath.Join(build.Dir, home)
	}
	return home, nil
}

var minimumGradleByJava = [...]uint64{
	8: 200, 9: 403, 10: 407, 11: 500, 12: 504,
	13: 600, 14: 603, 15: 607, 16: 700, 17: 703,
	18: 705, 19: 706, 20: 803, 21: 805, 22: 808,
	23: 810, 24: 814, 25: 901, 26: 904, 27: 908,
}

func gradleSupportsJava(version *semver.Version, major int) bool {
	if major < 8 || major >= len(minimumGradleByJava) {
		return false
	}
	if version == nil {
		return true
	}
	if version.Major() >= 9 && major < 17 {
		return false
	}
	return version.Major()*100+version.Minor() >= minimumGradleByJava[major]
}

func vendorMatches(required, actual string) bool {
	required = strings.ToLower(required)
	actual = strings.ToLower(actual)
	if required == "" || required == "any vendor" {
		return true
	}
	for _, pair := range [][2]string{
		{"adoptium", "eclipse"},
		{"azul", "azul"},
		{"zulu", "azul"},
		{"amazon", "amazon"},
		{"bellsoft", "bellsoft"},
		{"oracle", "oracle"},
		{"microsoft", "microsoft"},
		{"graal", "graal"},
		{"ibm", "ibm"},
	} {
		if strings.Contains(required, pair[0]) {
			return strings.Contains(actual, pair[1])
		}
	}
	return strings.Contains(actual, required)
}

func checkCompiler(project gradleProject, daemon toolchain.JavaInstallation) error {
	for _, requirement := range project.Compilers {
		major := daemon.Major
		executable := daemon.Javac
		if requirement.Executable != "" {
			executable = requirement.Executable
			major = requirement.Major
		}
		if executable == "" {
			return fmt.Errorf("%s needs javac, but the Gradle JVM is a runtime-only installation", requirement.Task)
		}
		info, err := os.Stat(executable)
		if err != nil {
			return fmt.Errorf("inspect compiler for %s: %w", requirement.Task, err)
		}
		if !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0) {
			return fmt.Errorf("compiler for %s is not executable: %s", requirement.Task, executable)
		}
		target := requirement.Release
		if target == 0 {
			if parsed, err := strconv.Atoi(strings.TrimPrefix(requirement.Target, "1.")); err == nil {
				target = parsed
			}
		}
		if target > major {
			return fmt.Errorf("%s targets Java %d, but its compiler is Java %d", requirement.Task, target, major)
		}
	}
	return nil
}

// toolchainWarnings reports projects that pin a Java toolchain no local JDK
// satisfies. Gradle enforces such a pin exactly: a newer JDK never substitutes,
// because its compiler emits newer bytecode than the project targets. Lucy turns
// Gradle's auto-provisioning off, so an unsatisfied pin otherwise surfaces as a
// "Cannot find a Java installation" error raised from inside a build script,
// naming a JDK the reader was never told to install.
//
// These are warnings rather than errors because the pin is read statically from
// a Groovy DSL that a convention plugin may also set, leaving Gradle the final
// authority. The check only moves the diagnosis ahead of the failure.
func toolchainWarnings(build buildrepo.GradleBuild, inventory toolchain.Inventory) []string {
	var warnings []string
	for _, project := range build.Projects {
		required := project.Java.Toolchain
		if required == nil || required.Major == 0 {
			continue
		}
		var available []string
		seen := map[int]bool{}
		satisfied := false
		for _, java := range inventory.Java {
			if java.Javac == "" {
				continue
			}
			if java.Major == required.Major && vendorMatches(required.Vendor, java.Vendor) {
				satisfied = true
			}
			if seen[java.Major] {
				continue
			}
			seen[java.Major] = true
			available = append(available, strconv.Itoa(java.Major))
		}
		if satisfied {
			continue
		}
		pin := fmt.Sprintf("%s pins a Java %d toolchain", project.Path, required.Major)
		if required.Vendor != "" {
			pin += " from " + required.Vendor
		}
		if len(available) == 0 {
			pin += ", but no local JDK was found"
		} else {
			pin += ", but local installations are Java " + strings.Join(available, ", ")
		}
		location := project.BuildFile
		if relative, err := filepath.Rel(build.Dir, project.BuildFile); err == nil {
			location = filepath.ToSlash(relative)
		}
		warnings = append(warnings, location+": "+pin)
	}
	return warnings
}
