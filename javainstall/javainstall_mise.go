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

// miseVendors maps each distribution to the vendor prefix of its mise java
// version.
var miseVendors = map[types.JavaDistribution]string{
	types.JavaDistributionTemurin:          "temurin",
	types.JavaDistributionZulu:             "zulu",
	types.JavaDistributionCorretto:         "corretto",
	types.JavaDistributionLiberica:         "liberica",
	types.JavaDistributionMicrosoft:        "microsoft",
	types.JavaDistributionOracle:           "oracle",
	types.JavaDistributionOracleOpenJDK:    "openjdk",
	types.JavaDistributionGraalVM:          "oracle-graalvm",
	types.JavaDistributionGraalVMCommunity: "graalvm-community",
	types.JavaDistributionSemeru:           "semeru",
	types.JavaDistributionSapMachine:       "sapmachine",
	types.JavaDistributionDragonwell:       "dragonwell",
	types.JavaDistributionKona:             "kona",
	types.JavaDistributionRedHat:           "redhat",
	types.JavaDistributionJetBrains:        "jetbrains",
}

type mise struct{}

func (mise) method() Method { return MethodMise }
func (mise) system() bool   { return false }

func (mise) ownsJava() bool {
	dir := miseDataDir()
	if dir == "" {
		return false
	}
	entries, err := os.ReadDir(filepath.Join(dir, "installs", "java"))
	return err == nil && len(entries) > 0
}

func (mise) plans(ctx context.Context, req Request) ([]Plan, error) {
	if _, err := exec.LookPath("mise"); err != nil {
		return nil, nil
	}
	prefix, distribution, ok := miseVendor(req.Distribution)
	if !ok {
		return nil, nil
	}
	spec := prefix + "-" + strconv.Itoa(req.Major)
	output, err := miseCommand(ctx, "ls-remote", "java", spec)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		log.Debug("mise java versions unavailable", "spec", spec, "reason", err)
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
func miseVendor(dist types.JavaDistribution) (string, types.JavaDistribution, bool) {
	if dist == types.JavaDistributionAny {
		return "temurin", types.JavaDistributionTemurin, true
	}
	prefix, ok := miseVendors[dist]
	return prefix, dist, ok
}

func miseDataDir() string {
	if dir := os.Getenv("MISE_DATA_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share", "mise")
}

// miseCommand runs a read-only mise query with a bounded wait.
func miseCommand(ctx context.Context, args ...string) (string, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	output, err := exec.CommandContext(probeCtx, "mise", args...).Output()
	return string(output), err
}
