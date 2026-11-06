package javainstall

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/mclucy/lucy/java"
)

// homebrewCasks maps a branded distribution to the base token of its macOS
// cask. Homebrew publishes no cask for the remaining distributions.
var homebrewCasks = map[java.Distribution]string{
	java.DistributionTemurin:    "temurin",
	java.DistributionZulu:       "zulu",
	java.DistributionCorretto:   "corretto",
	java.DistributionMicrosoft:  "microsoft-openjdk",
	java.DistributionOracle:     "oracle-jdk",
	java.DistributionGraalVM:    "graalvm-jdk",
	java.DistributionSapMachine: "sapmachine-jdk",
	java.DistributionSemeru:     "semeru-jdk-open",
}

type homebrew struct{}

func (homebrew) method() Method { return MethodHomebrew }
func (homebrew) system() bool   { return false }

func (homebrew) ownsJava(ctx context.Context, host *Host) bool {
	if host.isRoot() {
		return false
	}
	if _, err := host.lookPath("brew"); err != nil {
		return false
	}
	formulae, err := homebrewList(ctx, host, "--formula")
	if err == nil {
		for _, token := range strings.Fields(formulae) {
			if strings.HasPrefix(token, "openjdk") {
				return true
			}
		}
	}
	casks, err := homebrewList(ctx, host, "--cask")
	if err != nil {
		return false
	}
	for _, token := range strings.Fields(casks) {
		if homebrewJavaCask(token) {
			return true
		}
	}
	return false
}

func (homebrew) plans(ctx context.Context, req Request, host *Host) ([]Plan, error) {
	if host.goos != "darwin" && host.goos != "linux" {
		return nil, nil
	}
	if host.isRoot() {
		return nil, nil
	}
	if _, err := host.lookPath("brew"); err != nil {
		return nil, nil
	}
	if req.Distribution == java.DistributionAny {
		token, ok := homebrewFormula(ctx, host, req.Major)
		if !ok {
			return nil, nil
		}
		return []Plan{homebrewPlan(token, req.Major, java.DistributionAny, false)}, nil
	}
	if host.goos != "darwin" {
		return nil, nil
	}
	base, ok := homebrewCasks[req.Distribution]
	if !ok {
		return nil, nil
	}
	token, ok := homebrewCask(ctx, host, base, req.Major)
	if !ok {
		return nil, nil
	}
	return []Plan{homebrewPlan(token, req.Major, req.Distribution, true)}, nil
}

// homebrewFormula returns the formula that installs the requested major. The
// versioned formula is preferred; the unversioned openjdk formula qualifies
// only when its current major matches.
func homebrewFormula(ctx context.Context, host *Host, major int) (string, bool) {
	token := "openjdk@" + strconv.Itoa(major)
	if version, ok := homebrewBottledFormulaVersion(ctx, host, token); ok && homebrewMajor(version) == major {
		return token, true
	}
	version, ok := homebrewBottledFormulaVersion(ctx, host, "openjdk")
	if !ok || homebrewMajor(version) != major {
		return "", false
	}
	return "openjdk", true
}

// homebrewCask returns the cask that installs the requested major. A versioned
// cask is preferred; the base cask qualifies only when its current major
// matches.
func homebrewCask(ctx context.Context, host *Host, base string, major int) (string, bool) {
	for _, token := range []string{base + "@" + strconv.Itoa(major), base} {
		version, ok := homebrewCaskVersion(ctx, host, token)
		if ok && homebrewMajor(version) == major {
			return token, true
		}
	}
	return "", false
}

// homebrewBottledFormulaVersion returns the stable version of a formula that
// has a bottle. A formula without one compiles the JDK from source, which
// takes long and is not a native install.
func homebrewBottledFormulaVersion(ctx context.Context, host *Host, token string) (string, bool) {
	output, err := homebrewInfo(ctx, host, "--formula", token)
	if err != nil {
		if ctx.Err() == nil {
			host.logger.Debug("homebrew formula info unavailable", "formula", token, "reason", err)
		}
		return "", false
	}
	var info struct {
		Formulae []struct {
			Versions struct {
				Stable string `json:"stable"`
			} `json:"versions"`
			Bottle struct {
				Stable struct {
					Files map[string]json.RawMessage `json:"files"`
				} `json:"stable"`
			} `json:"bottle"`
		} `json:"formulae"`
	}
	if err := json.Unmarshal(output, &info); err != nil || len(info.Formulae) == 0 {
		return "", false
	}
	formula := info.Formulae[0]
	if len(formula.Bottle.Stable.Files) == 0 {
		host.logger.Debug(
			"homebrew formula skipped",
			"formula", token,
			"reason", "no bottle, so it would build from source",
		)
		return "", false
	}
	return formula.Versions.Stable, true
}

func homebrewCaskVersion(ctx context.Context, host *Host, token string) (string, bool) {
	output, err := homebrewInfo(ctx, host, "--cask", token)
	if err != nil {
		if ctx.Err() == nil {
			host.logger.Debug("homebrew cask info unavailable", "cask", token, "reason", err)
		}
		return "", false
	}
	var info struct {
		Casks []struct {
			Version string `json:"version"`
		} `json:"casks"`
	}
	if err := json.Unmarshal(output, &info); err != nil || len(info.Casks) == 0 {
		return "", false
	}
	return info.Casks[0].Version, true
}

// homebrewInfo reads local Homebrew metadata without updating taps.
func homebrewInfo(ctx context.Context, host *Host, kind, token string) ([]byte, error) {
	environment := append(host.environ(), "HOMEBREW_NO_AUTO_UPDATE=1")
	return host.probe(ctx, 30*time.Second, environment, "brew", "info", "--json=v2", kind, token)
}

func homebrewList(ctx context.Context, host *Host, kind string) (string, error) {
	output, err := host.probe(ctx, 20*time.Second, nil, "brew", "list", kind)
	return string(output), err
}

// homebrewMajor returns the leading major of a formula or cask version, which
// may carry a revision after a comma. Java 8 builds use the legacy 1.8 scheme.
func homebrewMajor(version string) int {
	version = strings.TrimPrefix(version, "1.")
	end := 0
	for end < len(version) && version[end] >= '0' && version[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0
	}
	major, err := strconv.Atoi(version[:end])
	if err != nil {
		return 0
	}
	return major
}

func homebrewJavaCask(token string) bool {
	for _, base := range []string{
		"temurin",
		"zulu",
		"corretto",
		"microsoft-openjdk",
		"oracle-jdk",
		"graalvm-jdk",
		"sapmachine-jdk",
		"semeru-jdk-open",
	} {
		if token == base || strings.HasPrefix(token, base+"@") {
			return true
		}
	}
	return false
}

func homebrewPlan(token string, major int, distribution java.Distribution, cask bool) Plan {
	args := []string{"brew", "install"}
	elevation := ElevationNone
	if cask {
		args = append(args, "--cask")
		// The .pkg installer asks for the password itself.
		elevation = ElevationSelf
	}
	args = append(args, token)
	return Plan{
		Method:       MethodHomebrew,
		Package:      token,
		Distribution: distribution,
		Major:        major,
		Steps: []Step{{
			Args:      args,
			Elevation: elevation,
		}},
	}
}
