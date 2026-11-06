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

// miseVendors maps each distribution to the vendor prefix of its mise java
// version.
var miseVendors = map[java.Distribution]string{
	java.DistributionTemurin:          "temurin",
	java.DistributionZulu:             "zulu",
	java.DistributionCorretto:         "corretto",
	java.DistributionLiberica:         "liberica",
	java.DistributionMicrosoft:        "microsoft",
	java.DistributionOracle:           "oracle",
	java.DistributionOracleOpenJDK:    "openjdk",
	java.DistributionGraalVM:          "oracle-graalvm",
	java.DistributionGraalVMCommunity: "graalvm-community",
	java.DistributionSemeru:           "semeru",
	java.DistributionSapMachine:       "sapmachine",
	java.DistributionDragonwell:       "dragonwell",
	java.DistributionKona:             "kona",
	java.DistributionRedHat:           "redhat",
	java.DistributionJetBrains:        "jetbrains",
}

type mise struct{}

func (mise) method() Method { return MethodMise }
func (mise) system() bool   { return false }

func (mise) ownsJava(_ context.Context, host *Host) bool {
	dir := miseDataDir(host)
	if dir == "" {
		return false
	}
	entries, err := os.ReadDir(filepath.Join(dir, "installs", "java"))
	return err == nil && len(entries) > 0
}

func (mise) plans(ctx context.Context, req Request, host *Host) ([]Plan, error) {
	if _, err := host.lookPath("mise"); err != nil {
		return nil, nil
	}
	prefix, distribution, ok := miseVendor(req.Distribution)
	if !ok {
		return nil, nil
	}
	spec := prefix + "-" + strconv.Itoa(req.Major)
	output, err := miseCommand(ctx, host, "ls-remote", "java", spec)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		host.logger.Debug("mise java versions unavailable", "spec", spec, "reason", err)
		return nil, nil
	}
	if strings.TrimSpace(output) == "" {
		return nil, nil
	}
	return []Plan{{
		Method:       MethodMise,
		Package:      "java@" + spec,
		Distribution: distribution,
		Major:        req.Major,
		Steps: []Step{{
			Args:      []string{"mise", "install", "java@" + spec},
			Elevation: ElevationNone,
		}},
	}}, nil
}

// miseVendor returns the vendor prefix for dist. The unspecified distribution
// installs Temurin, the mise java default.
func miseVendor(dist java.Distribution) (string, java.Distribution, bool) {
	if dist == java.DistributionAny {
		return "temurin", java.DistributionTemurin, true
	}
	prefix, ok := miseVendors[dist]
	return prefix, dist, ok
}

func miseDataDir(host *Host) string {
	if dir := host.getenv("MISE_DATA_DIR"); dir != "" {
		return dir
	}
	home, err := host.userHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share", "mise")
}

// miseCommand runs a read-only mise query with a bounded wait.
func miseCommand(
	ctx context.Context,
	host *Host,
	args ...string,
) (string, error) {
	output, err := host.probe(ctx, 20*time.Second, nil, "mise", args...)
	return string(output), err
}
