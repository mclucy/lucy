package javainstall

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/types"
)

// asdfVendors maps each distribution to the vendor prefix of its asdf-java
// version. asdf-java has no Red Hat build.
var asdfVendors = map[types.JavaDistribution]string{
	types.JavaDistributionTemurin:          "temurin",
	types.JavaDistributionZulu:             "zulu",
	types.JavaDistributionCorretto:         "corretto",
	types.JavaDistributionLiberica:         "liberica",
	types.JavaDistributionMicrosoft:        "microsoft",
	types.JavaDistributionOracle:           "oracle",
	types.JavaDistributionOracleOpenJDK:    "openjdk",
	types.JavaDistributionGraalVM:          "oracle-graalvm",
	types.JavaDistributionGraalVMCommunity: "graalvm-community",
	types.JavaDistributionSemeru:           "semeru-openj9",
	types.JavaDistributionSapMachine:       "sapmachine",
	types.JavaDistributionDragonwell:       "dragonwell",
	types.JavaDistributionKona:             "kona",
	types.JavaDistributionJetBrains:        "jetbrains",
}

type asdf struct{}

func (asdf) method() Method { return MethodAsdf }
func (asdf) system() bool   { return false }

func (asdf) ownsJava() bool {
	dir := asdfDataDir()
	if dir == "" {
		return false
	}
	entries, err := os.ReadDir(filepath.Join(dir, "installs", "java"))
	return err == nil && len(entries) > 0
}

func (asdf) plans(ctx context.Context, req Request) ([]Plan, error) {
	if _, err := exec.LookPath("asdf"); err != nil {
		return nil, nil
	}
	plugins, err := asdfCommand(ctx, "plugin", "list")
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		log.Debug("asdf plugins unavailable", "reason", err)
		return nil, nil
	}
	if !asdfHasJavaPlugin(plugins) {
		return nil, nil
	}
	prefix, distribution, ok := asdfVendor(req.Distribution)
	if !ok {
		return nil, nil
	}
	spec := prefix + "-" + strconv.Itoa(req.Major)
	versions, err := asdfCommand(ctx, "list", "all", "java", spec)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		log.Debug("asdf java versions unavailable", "spec", spec, "reason", err)
		return nil, nil
	}
	if strings.TrimSpace(versions) == "" {
		return nil, nil
	}
	return []Plan{{
		Method:       MethodAsdf,
		Package:      "java@" + spec,
		Distribution: distribution,
		Major:        req.Major,
		Steps: []Step{{
			Args:      []string{"asdf", "install", "java", "latest:" + spec},
			Elevation: ElevationNone,
		}},
	}}, nil
}

// asdfVendor returns the vendor prefix for dist. The unspecified distribution
// installs Temurin, the plugin's default build.
func asdfVendor(dist types.JavaDistribution) (string, types.JavaDistribution, bool) {
	if dist == types.JavaDistributionAny {
		return "temurin", types.JavaDistributionTemurin, true
	}
	prefix, ok := asdfVendors[dist]
	return prefix, dist, ok
}

func asdfDataDir() string {
	if dir := os.Getenv("ASDF_DATA_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".asdf")
}

// asdfHasJavaPlugin reports whether the java plugin is installed. asdf prints
// one plugin name per line.
func asdfHasJavaPlugin(output string) bool {
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == "java" {
			return true
		}
	}
	return false
}

// asdfCommand runs a read-only asdf query with a bounded wait.
func asdfCommand(ctx context.Context, args ...string) (string, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	output, err := exec.CommandContext(probeCtx, "asdf", args...).Output()
	return string(output), err
}
