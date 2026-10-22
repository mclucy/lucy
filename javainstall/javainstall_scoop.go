package javainstall

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/mclucy/lucy/types"
)

// scoop is the Scoop package manager.
type scoop struct{}

// scoopJavaBucketURL is the repository that serves the java bucket.
const scoopJavaBucketURL = "https://github.com/ScoopInstaller/Java"

// scoopJavaPackage describes the manifests of one distribution in the java
// bucket. The formats carry one %d directive for the major. An empty format
// marks a build the bucket does not package.
type scoopJavaPackage struct {
	distribution types.JavaDistribution
	jreFormat    string
	jdkFormat    string
	jreMajors    []int
	jdkMajors    []int
}

// scoopJavaPackages lists the java bucket manifests that exist for each
// distribution.
var scoopJavaPackages = []scoopJavaPackage{
	{
		distribution: types.JavaDistributionTemurin,
		jreFormat:    "temurin%d-jre",
		jdkFormat:    "temurin%d-jdk",
		jreMajors:    []int{8, 11, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26},
		jdkMajors:    []int{8, 11, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26},
	},
	{
		distribution: types.JavaDistributionZulu,
		jreFormat:    "zulu%d-jre",
		jdkFormat:    "zulu%d-jdk",
		jreMajors:    []int{8, 11, 12, 13, 14, 15, 16, 17, 21, 22},
		jdkMajors:    []int{7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 21, 22, 25},
	},
	{
		distribution: types.JavaDistributionCorretto,
		jreFormat:    "corretto%d-jre",
		jdkFormat:    "corretto%d-jdk",
		jreMajors:    []int{8},
		jdkMajors:    []int{8, 11, 15, 16, 17, 18, 19, 20, 21, 22, 23, 25},
	},
	{
		distribution: types.JavaDistributionLiberica,
		jreFormat:    "liberica%d-jre",
		jdkFormat:    "liberica%d-jdk",
		jreMajors:    []int{8, 11, 13, 14, 15, 16, 17, 21, 25},
		jdkMajors:    []int{8, 11, 12, 13, 14, 15, 16, 17, 21, 25},
	},
	{
		distribution: types.JavaDistributionMicrosoft,
		jdkFormat:    "microsoft%d-jdk",
		jdkMajors:    []int{11, 16, 17, 21},
	},
	{
		distribution: types.JavaDistributionSapMachine,
		jreFormat:    "sapmachine%d-jre",
		jdkFormat:    "sapmachine%d-jdk",
		jreMajors:    []int{11, 12, 13, 14, 15, 16, 17, 18},
		jdkMajors:    []int{11, 12, 13, 14, 15, 16, 17, 18},
	},
	{
		distribution: types.JavaDistributionSemeru,
		jreFormat:    "semeru%d-jre",
		jdkFormat:    "semeru%d-jdk",
		jreMajors:    []int{8, 11, 16, 17, 18, 19, 20, 21, 22, 23},
		jdkMajors:    []int{8, 11, 16, 17, 18, 19, 20, 21, 22, 23},
	},
	{
		distribution: types.JavaDistributionGraalVM,
		jdkFormat:    "graalvm-oracle-%djdk",
		jdkMajors:    []int{17, 20, 21, 22, 23, 24, 25},
	},
	{
		distribution: types.JavaDistributionGraalVMCommunity,
		jdkFormat:    "graalvm%d-jdk",
		jdkMajors:    []int{17, 20, 21, 22, 23, 24, 25},
	},
	{
		distribution: types.JavaDistributionDragonwell,
		jdkFormat:    "dragonwell%d-jdk",
		jdkMajors:    []int{8, 11, 17, 21},
	},
	{
		distribution: types.JavaDistributionRedHat,
		jreFormat:    "openjdk%d-redhat-jre",
		jdkFormat:    "openjdk%d-redhat",
		jreMajors:    []int{8},
		jdkMajors:    []int{8},
	},
	{
		distribution: types.JavaDistributionJetBrains,
		jdkFormat:    "intellij-jbr%d",
		jdkMajors:    []int{11, 17, 21, 25},
	},
}

// scoopOpenJDKManifestMajors lists the majors with an unbranded openjdk
// manifest. The bucket serves the manifest as openjdk<major>.
var scoopOpenJDKManifestMajors = []int{
	9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27,
}

// scoopCandidate is one manifest that can satisfy a request.
type scoopCandidate struct {
	manifest     string
	distribution types.JavaDistribution
}

func (scoop) method() Method { return MethodScoop }

func (scoop) plans(ctx context.Context, req Request) ([]Plan, error) {
	if runtime.GOOS != "windows" {
		return nil, nil
	}
	if _, err := exec.LookPath("scoop.cmd"); err != nil {
		return nil, nil
	}
	hasManifest, err := scoopJavaBucket(ctx)
	if err != nil {
		return nil, err
	}
	return scoopPlans(req, hasManifest), nil
}

func (scoop) system() bool { return false }

// ownsJava reports whether scoop installed a Java runtime. Every scoop app
// links its active version through a current directory.
func (scoop) ownsJava() bool {
	if runtime.GOOS != "windows" {
		return false
	}
	root := scoopRoot()
	if root == "" {
		return false
	}
	matches, err := filepath.Glob(
		filepath.Join(root, "apps", "*", "current", "bin", "java.exe"),
	)
	return err == nil && len(matches) > 0
}

// scoopPlans returns the plan that the java bucket installs req. At most one
// plan results. hasManifest reports whether the installed bucket has a
// manifest. A nil hasManifest means the bucket is absent, so the plan adds it
// and trusts the manifest names.
func scoopPlans(req Request, hasManifest func(string) bool) []Plan {
	var chosen scoopCandidate
	found := false
	for _, candidate := range scoopCandidates(req) {
		if hasManifest == nil || hasManifest(candidate.manifest) {
			chosen = candidate
			found = true
			break
		}
	}
	if !found {
		return nil
	}
	plan := Plan{
		Method:       MethodScoop,
		Package:      "java/" + chosen.manifest,
		Distribution: chosen.distribution,
		Major:        req.Major,
		Steps: []Step{{
			Args:      []string{"scoop", "install", "java/" + chosen.manifest},
			Elevation: ElevationNone,
		}},
	}
	if hasManifest == nil {
		plan.Repository = &Repository{
			Name: "Scoop java bucket",
			URL:  scoopJavaBucketURL,
			Steps: []Step{{
				Args:      []string{"scoop", "bucket", "add", "java"},
				Elevation: ElevationNone,
			}},
		}
	}
	return []Plan{plan}
}

// scoopCandidates lists the manifests that can install req, preferred first. A
// request for any build prefers Temurin and falls back to the unbranded
// openjdk manifest.
func scoopCandidates(req Request) []scoopCandidate {
	distribution := req.Distribution
	if distribution == types.JavaDistributionAny {
		distribution = types.JavaDistributionTemurin
	}
	var candidates []scoopCandidate
	for _, pkg := range scoopJavaPackages {
		if pkg.distribution == distribution {
			candidates = append(
				candidates,
				scoopPackageCandidates(pkg, req.Major)...,
			)
			break
		}
	}
	if req.Distribution == types.JavaDistributionAny &&
		slices.Contains(scoopOpenJDKManifestMajors, req.Major) {
		candidates = append(candidates, scoopCandidate{
			manifest:     fmt.Sprintf("openjdk%d", req.Major),
			distribution: types.JavaDistributionAny,
		})
	}
	return candidates
}

// scoopPackageCandidates returns the JRE manifest before the JDK manifest.
func scoopPackageCandidates(pkg scoopJavaPackage, major int) []scoopCandidate {
	var candidates []scoopCandidate
	if pkg.jreFormat != "" && slices.Contains(pkg.jreMajors, major) {
		candidates = append(candidates, scoopCandidate{
			manifest:     fmt.Sprintf(pkg.jreFormat, major),
			distribution: pkg.distribution,
		})
	}
	if pkg.jdkFormat != "" && slices.Contains(pkg.jdkMajors, major) {
		candidates = append(candidates, scoopCandidate{
			manifest:     fmt.Sprintf(pkg.jdkFormat, major),
			distribution: pkg.distribution,
		})
	}
	return candidates
}

// scoopJavaBucket returns a manifest check for the installed java bucket. A
// nil check means the bucket is absent.
func scoopJavaBucket(ctx context.Context) (func(string) bool, error) {
	cmd := exec.CommandContext(
		ctx,
		"cmd.exe",
		"/d",
		"/s",
		"/c",
		"scoop bucket list",
	)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("list scoop buckets: %w", err)
	}
	if !scoopHasBucket(string(output), "java") {
		return nil, nil
	}
	root := scoopRoot()
	if root == "" {
		return nil, nil
	}
	dir := filepath.Join(root, "buckets", "java", "bucket")
	return func(manifest string) bool {
		info, err := os.Stat(filepath.Join(dir, manifest+".json"))
		return err == nil && info.Mode().IsRegular()
	}, nil
}

// scoopHasBucket reports whether the bucket list output names bucket. The
// first column of each row is the bucket name.
func scoopHasBucket(output, bucket string) bool {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == bucket {
			return true
		}
	}
	return false
}

// scoopRoot returns the scoop installation directory.
func scoopRoot() string {
	if root := os.Getenv("SCOOP"); root != "" {
		return root
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "scoop")
}
