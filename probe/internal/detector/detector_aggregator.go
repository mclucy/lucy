package detector

import (
	"archive/zip"
	"os"

	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/tools"
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
