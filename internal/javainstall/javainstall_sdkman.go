package javainstall

import (
	"cmp"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mclucy/lucy/internal/shell"
	"github.com/mclucy/lucy/java"
)

// sdkmanVendors maps the vendor suffix of an SDKMAN candidate identifier to the
// build it names. SDKMAN has no vendor suffix for the remaining distributions.
var sdkmanVendors = map[string]java.Distribution{
	"tem":     java.DistributionTemurin,
	"zulu":    java.DistributionZulu,
	"amzn":    java.DistributionCorretto,
	"librca":  java.DistributionLiberica,
	"ms":      java.DistributionMicrosoft,
	"oracle":  java.DistributionOracle,
	"open":    java.DistributionOracleOpenJDK,
	"graalce": java.DistributionGraalVMCommunity,
	"graal":   java.DistributionGraalVM,
	"sem":     java.DistributionSemeru,
	"sapmchn": java.DistributionSapMachine,
	"kona":    java.DistributionKona,
	"jbr":     java.DistributionJetBrains,
}

type sdkman struct{}

func (sdkman) method() Method { return MethodSDKMAN }
func (sdkman) system() bool   { return false }

func (sdkman) ownsJava(_ context.Context, host *Host) bool {
	home, ok := sdkmanHome(host)
	if !ok {
		return false
	}
	entries, err := os.ReadDir(filepath.Join(home, "candidates", "java"))
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.Name() != "current" {
			return true
		}
	}
	return false
}

func (sdkman) plans(ctx context.Context, req Request, host *Host) ([]Plan, error) {
	if host.goos == "windows" {
		return nil, nil
	}
	home, ok := sdkmanHome(host)
	if !ok {
		return nil, nil
	}
	initScript := filepath.Join(home, "bin", "sdkman-init.sh")
	if _, err := os.Stat(initScript); err != nil {
		return nil, nil
	}
	platform, ok := sdkmanPlatform(host.goos, host.goarch)
	if !ok {
		return nil, nil
	}
	versions, err := sdkmanVersions(ctx, host, platform)
	if err != nil {
		host.logger.Debug(
			"sdkman java versions unavailable",
			"platform", platform,
			"reason", err,
		)
		return nil, nil
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	id, distribution, ok := sdkmanPick(versions, req)
	if !ok {
		return nil, nil
	}
	return []Plan{{
		Method:       MethodSDKMAN,
		Package:      id,
		Distribution: distribution,
		Major:        req.Major,
		Steps: []Step{{
			Args: []string{
				"bash",
				"-c",
				"source " + shell.Quote(initScript) +
					" && sdk install java " + shell.Quote(id),
			},
			Elevation: ElevationNone,
			// "n" keeps SDKMAN from making the new candidate the default.
			Input: "n\n",
		}},
	}}, nil
}

// sdkmanHome returns the SDKMAN directory. It is unavailable when neither
// SDKMAN_DIR nor the home directory names one.
func sdkmanHome(host *Host) (string, bool) {
	if dir := host.getenv("SDKMAN_DIR"); dir != "" {
		return dir, true
	}
	home, err := host.userHomeDir()
	if err != nil {
		return "", false
	}
	return filepath.Join(home, ".sdkman"), true
}

// sdkmanPlatform returns the SDKMAN platform segment for this host.
func sdkmanPlatform(goos, goarch string) (string, bool) {
	switch goos + "/" + goarch {
	case "darwin/arm64":
		return "darwinarm64", true
	case "darwin/amd64":
		return "darwinx64", true
	case "linux/amd64":
		return "linuxx64", true
	case "linux/arm64":
		return "linuxarm64", true
	default:
		return "", false
	}
}

func sdkmanVersions(
	ctx context.Context,
	host *Host,
	platform string,
) ([]string, error) {
	data, err := host.fetchMetadata(
		ctx,
		"https://api.sdkman.io/2/candidates/java/"+platform+"/versions/all",
		time.Hour,
	)
	if err != nil {
		return nil, err
	}
	return strings.Split(string(data), ","), nil
}

// sdkmanPick returns the newest plain candidate for the request. A candidate
// is plain when it is exactly a version and one vendor suffix.
func sdkmanPick(versions []string, req Request) (string, java.Distribution, bool) {
	wanted := req.Distribution
	if wanted == java.DistributionAny {
		wanted = java.DistributionTemurin
	}
	best := ""
	bestVersion := []int(nil)
	for _, raw := range versions {
		id := strings.TrimSpace(raw)
		version, vendor, ok := sdkmanSplit(id)
		if !ok {
			continue
		}
		distribution, ok := sdkmanVendors[vendor]
		if !ok || distribution != wanted {
			continue
		}
		numbers := sdkmanNumbers(version)
		if len(numbers) == 0 || numbers[0] != req.Major {
			continue
		}
		if best == "" || sdkmanCompareVersions(numbers, bestVersion) > 0 {
			best = id
			bestVersion = numbers
		}
	}
	return best, wanted, best != ""
}

// sdkmanSplit discards variant builds, which carry a second dash or a variant
// tag in the version.
func sdkmanSplit(id string) (string, string, bool) {
	if strings.Count(id, "-") != 1 {
		return "", "", false
	}
	version, vendor, ok := strings.Cut(id, "-")
	if !ok || version == "" || vendor == "" {
		return "", "", false
	}
	if strings.Contains(version, "fx") || strings.Contains(version, "crac") {
		return "", "", false
	}
	return version, vendor, true
}

func sdkmanNumbers(version string) []int {
	fields := strings.FieldsFunc(version, func(r rune) bool {
		return r < '0' || r > '9'
	})
	numbers := make([]int, 0, len(fields))
	for _, field := range fields {
		number, err := strconv.Atoi(field)
		if err != nil {
			continue
		}
		numbers = append(numbers, number)
	}
	return numbers
}

func sdkmanCompareVersions(a, b []int) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if result := cmp.Compare(a[i], b[i]); result != 0 {
			return result
		}
	}
	return cmp.Compare(len(a), len(b))
}
