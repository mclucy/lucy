package javainstall

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mclucy/lucy/java"
)

// asdfVendors maps each distribution to the vendor prefix of its asdf-java
// version. asdf-java has no Red Hat build.
var asdfVendors = map[java.Distribution]string{
	java.DistributionTemurin:          "temurin",
	java.DistributionZulu:             "zulu",
	java.DistributionCorretto:         "corretto",
	java.DistributionLiberica:         "liberica",
	java.DistributionMicrosoft:        "microsoft",
	java.DistributionOracle:           "oracle",
	java.DistributionOracleOpenJDK:    "openjdk",
	java.DistributionGraalVM:          "oracle-graalvm",
	java.DistributionGraalVMCommunity: "graalvm-community",
	java.DistributionSemeru:           "semeru-openj9",
	java.DistributionSapMachine:       "sapmachine",
	java.DistributionDragonwell:       "dragonwell",
	java.DistributionKona:             "kona",
	java.DistributionJetBrains:        "jetbrains",
}

type asdf struct{}

func (asdf) method() Method { return MethodAsdf }
func (asdf) system() bool   { return false }

func (asdf) ownsJava(_ context.Context, host *Host) bool {
	dir := asdfDataDir(host)
	if dir == "" {
		return false
	}
	entries, err := os.ReadDir(filepath.Join(dir, "installs", "java"))
	return err == nil && len(entries) > 0
}

func (asdf) plans(ctx context.Context, req Request, host *Host) ([]Plan, error) {
	if _, err := host.lookPath("asdf"); err != nil {
		return nil, nil
	}
	plugins, err := asdfCommand(ctx, host, "plugin", "list")
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		host.logger.Debug("asdf plugins unavailable", "reason", err)
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
	versions, err := asdfCommand(ctx, host, "list", "all", "java", spec)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		host.logger.Debug("asdf java versions unavailable", "spec", spec, "reason", err)
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
func asdfVendor(dist java.Distribution) (string, java.Distribution, bool) {
	if dist == java.DistributionAny {
		return "temurin", java.DistributionTemurin, true
	}
	prefix, ok := asdfVendors[dist]
	return prefix, dist, ok
}

func asdfDataDir(host *Host) string {
	if dir := host.getenv("ASDF_DATA_DIR"); dir != "" {
		return dir
	}
	home, err := host.userHomeDir()
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
func asdfCommand(
	ctx context.Context,
	host *Host,
	args ...string,
) (string, error) {
	output, err := host.probe(ctx, 20*time.Second, nil, "asdf", args...)
	return string(output), err
}
