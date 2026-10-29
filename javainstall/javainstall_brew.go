package javainstall

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/mclucy/lucy/java"
	"github.com/mclucy/lucy/log"
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

func (homebrew) ownsJava() bool {
	if os.Geteuid() == 0 {
		return false
	}
	if _, err := exec.LookPath("brew"); err != nil {
		return false
	}
	formulae, err := homebrewList(context.Background(), "--formula")
	if err == nil {
		for _, token := range strings.Fields(formulae) {
			if strings.HasPrefix(token, "openjdk") {
				return true
			}
		}
	}
	casks, err := homebrewList(context.Background(), "--cask")
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

func (homebrew) plans(ctx context.Context, req Request) ([]Plan, error) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return nil, nil
	}
	if os.Geteuid() == 0 {
		return nil, nil
	}
	if _, err := exec.LookPath("brew"); err != nil {
		return nil, nil
	}
	if req.Distribution == java.DistributionAny {
		token, ok := homebrewFormula(ctx, req.Major)
		if !ok {
			return nil, nil
		}
		return []Plan{homebrewPlan(token, req.Major, java.DistributionAny, false)}, nil
	}
	if runtime.GOOS != "darwin" {
		return nil, nil
	}
	base, ok := homebrewCasks[req.Distribution]
	if !ok {
		return nil, nil
	}
	token, ok := homebrewCask(ctx, base, req.Major)
	if !ok {
		return nil, nil
	}
	return []Plan{homebrewPlan(token, req.Major, req.Distribution, true)}, nil
}

// homebrewFormula returns the formula that installs the requested major. The
// versioned formula is preferred; the unversioned openjdk formula qualifies
// only when its current major matches.
func homebrewFormula(ctx context.Context, major int) (string, bool) {
	token := "openjdk@" + strconv.Itoa(major)
	if version, ok := homebrewBottledFormulaVersion(ctx, token); ok && homebrewMajor(version) == major {
		return token, true
	}
	version, ok := homebrewBottledFormulaVersion(ctx, "openjdk")
	if !ok || homebrewMajor(version) != major {
		return "", false
	}
	return "openjdk", true
}

// homebrewCask returns the cask that installs the requested major. A versioned
// cask is preferred; the base cask qualifies only when its current major
// matches.
func homebrewCask(ctx context.Context, base string, major int) (string, bool) {
	for _, token := range []string{base + "@" + strconv.Itoa(major), base} {
		version, ok := homebrewCaskVersion(ctx, token)
		if ok && homebrewMajor(version) == major {
			return token, true
		}
	}
	return "", false
}

// homebrewBottledFormulaVersion returns the stable version of a formula that
// has a bottle. A formula without one compiles the JDK from source, which
// takes long and is not a native install.
func homebrewBottledFormulaVersion(ctx context.Context, token string) (string, bool) {
	output, err := homebrewInfo(ctx, "--formula", token)
	if err != nil {
		if ctx.Err() == nil {
			log.Debug("homebrew formula info unavailable", "formula", token, "reason", err)
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
		log.Debug(
			"homebrew formula skipped",
			"formula", token,
			"reason", "no bottle, so it would build from source",
		)
		return "", false
	}
	return formula.Versions.Stable, true
}

func homebrewCaskVersion(ctx context.Context, token string) (string, bool) {
	output, err := homebrewInfo(ctx, "--cask", token)
	if err != nil {
		if ctx.Err() == nil {
			log.Debug("homebrew cask info unavailable", "cask", token, "reason", err)
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
func homebrewInfo(ctx context.Context, kind, token string) ([]byte, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	command := exec.CommandContext(probeCtx, "brew", "info", "--json=v2", kind, token)
	command.Env = append(os.Environ(), "HOMEBREW_NO_AUTO_UPDATE=1")
	return command.Output()
}

func homebrewList(ctx context.Context, kind string) (string, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	output, err := exec.CommandContext(probeCtx, "brew", "list", kind).Output()
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
