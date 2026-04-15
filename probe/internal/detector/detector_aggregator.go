package detector

import (
	"archive/zip"
	"fmt"
	"os"
	"path"

	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/tools"
	"github.com/mclucy/lucy/types"
)

// Executable analyzes a JAR file using all registered detectors
// and returns the first successful match (in registration order).
// If multiple detectors match, callers should handle ambiguity separately.
func Executable(filePath string) *types.RuntimeInfo {
	file, err := os.Open(filePath)
	if err != nil {
		logger.Debug("Failed to open file: " + err.Error())
		return nil
	}
	defer tools.CloseReader(file, logger.Warn)

	stat, err := file.Stat()
	if err != nil {
		logger.Debug("Failed to stat file: " + err.Error())
		return nil
	}

	zipReader, err := zip.NewReader(file, stat.Size())
	if err != nil {
		logger.Debug("Failed to read JAR file: " + err.Error())
		return nil
	}

	var candidates []*types.RuntimeInfo
	detectors := getExecutableDetectors()

	for _, detector := range detectors {
		result, err := detector.Detect(filePath, zipReader, file)
		if err != nil || result == nil {
			continue
		}
		candidates = append(candidates, result)
	}

	if len(candidates) == 1 {
		bridgeMarkers := DetectBridgeMarkers(zipReader)
		if len(bridgeMarkers) > 0 {
			candidates[0].BridgeHints = make([]string, 0, len(bridgeMarkers))
			for _, marker := range bridgeMarkers {
				candidates[0].BridgeHints = append(
					candidates[0].BridgeHints,
					marker.NodeID,
				)
			}
		}
	}

	if len(candidates) == 0 {
		return types.NoExecutable
	}

	if len(candidates) > 1 {
		// TODO: Modify this by need to handle multiple matches better
		logger.Warn(fmt.Errorf("multiple executable detectors matched; marking as unknown"))
		return types.UnknownExecutable
	}

	return candidates[0]
}

// Packages analyzes a mod/plugin file and returns detected packages.
// Cross-ecosystem conflicts within a single JAR are resolved here per the
// precedence policy defined in probe/probe_topology_enrich.go. If detected
// packages span two incompatible ecosystem families (e.g. proxy + server), the
// result is nil — callers treat the file as unresolved rather than guessing.
func Packages(filePath string) (res []types.Package) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil
	}
	defer tools.CloseReader(file, logger.Warn)

	stat, err := file.Stat()
	if err != nil {
		return nil
	}

	switch path.Ext(filePath) {
	case ".jar", ".zip":
		zipReader, err := zip.NewReader(file, stat.Size())
		if err != nil {
			return nil
		}
		for _, detector := range getModDetectors() {
			result, err := detector.Detect(zipReader, file)
			if err != nil || result == nil {
				continue
			}
			res = append(res, result...)
		}
		if jarPlatformsConflict(res) {
			logger.Warn(fmt.Errorf(
				"ambiguous JAR %q: packages span incompatible ecosystems, treating as unresolved",
				filePath,
			))
			return nil
		}
	case ".pyz", ".mcdr":
		McdrPlugin(filePath)
	default:
		return nil
	}

	return
}

// jarPlatformsConflict returns true when the detected packages span two or more
// ecosystem families that cannot coexist in a single deployable JAR.
//
// Ecosystem families (mirror the policy in probe/probe_topology_enrich.go):
//
//	proxyFamily  – velocity, bungeecord
//	serverFamily – bukkit, paper, leaves, folia, spigot
//	modFamily    – fabric, forge, neoforge
//
// PlatformAny packages (e.g. Sponge plugins) are intentionally excluded from
// the conflict check because they do not signal a specific incompatible family.
func jarPlatformsConflict(pkgs []types.Package) bool {
	if len(pkgs) == 0 {
		return false
	}

	proxyPlatforms := map[types.Platform]struct{}{
		types.Platform("velocity"):   {},
		types.Platform("bungeecord"): {},
	}
	serverPlatforms := map[types.Platform]struct{}{
		types.Platform("bukkit"): {},
		types.Platform("paper"):  {},
		types.Platform("leaves"): {},
		types.Platform("folia"):  {},
		types.Platform("spigot"): {},
	}
	modPlatforms := map[types.Platform]struct{}{
		types.PlatformFabric:   {},
		types.PlatformForge:    {},
		types.PlatformNeoforge: {},
	}

	var hasProxy, hasServer, hasMod bool
	for _, pkg := range pkgs {
		p := pkg.Id.Platform
		if p == types.PlatformAny {
			continue
		}
		if _, ok := proxyPlatforms[p]; ok {
			hasProxy = true
		}
		if _, ok := serverPlatforms[p]; ok {
			hasServer = true
		}
		if _, ok := modPlatforms[p]; ok {
			hasMod = true
		}
	}

	families := 0
	if hasProxy {
		families++
	}
	if hasServer {
		families++
	}
	if hasMod {
		families++
	}
	return families > 1
}

func McdrPlugin(filePath string) (res []types.Package) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil
	}
	defer tools.CloseReader(file, logger.Warn)

	stat, err := file.Stat()
	if err != nil {
		return nil
	}

	zipReader, err := zip.NewReader(file, stat.Size())
	if err != nil {
		return nil
	}

	detector := getOtherPackageDetectors()["mcdr plugin"]
	result, err := detector.Detect(zipReader, file)
	if err != nil || result == nil {
		return nil
	}
	res = append(res, result...)

	return
}

// Environment checks for environment indicators (like MCDR)
func Environment(dir string) (env types.EnvironmentInfo) {
	detectors := getEnvironmentDetectors()
	for _, detector := range detectors {
		detector.Detect(dir, &env)
	}
	return
}
