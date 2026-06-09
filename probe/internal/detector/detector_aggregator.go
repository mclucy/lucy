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

// Executable analyzes a JAR file using all registered detectors and collects
// all executable evidence candidates in registration order.
func Executable(filePath string) *ExecutableCandidates {
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

	bridgeMarkers := DetectBridgeMarkers(zipReader)
	candidates := &ExecutableCandidates{
		Candidates: make([]*ExecutableEvidence, 0),
	}
	detectors := getExecutableDetectors()

	for _, detector := range detectors {
		result, err := detector.Detect(filePath, zipReader, file)
		if err != nil || result == nil {
			continue
		}
		result.BridgeHints = mergeBridgeHints(result.BridgeHints, bridgeMarkers)
		candidates.Candidates = append(candidates.Candidates, result)
	}

	return candidates
}

func mergeBridgeHints(existing []string, markers []BridgeMarker) []string {
	if len(existing) == 0 && len(markers) == 0 {
		return nil
	}

	merged := make([]string, 0, len(existing)+len(markers))
	seen := make(map[string]struct{}, len(existing)+len(markers))
	for _, hint := range existing {
		if _, ok := seen[hint]; ok {
			continue
		}
		seen[hint] = struct{}{}
		merged = append(merged, hint)
	}
	for _, marker := range markers {
		if _, ok := seen[marker.NodeID]; ok {
			continue
		}
		seen[marker.NodeID] = struct{}{}
		merged = append(merged, marker.NodeID)
	}

	return merged
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
		res = aggregateBukkitFamilyPackages(res)
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
