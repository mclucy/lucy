package detector

import (
	"archive/zip"
	"os"

	"github.com/mclucy/lucy/internal/fn"
	"github.com/mclucy/lucy/log"
)

// Executable analyzes a JAR file using all registered detectors and collects
// all executable evidence candidates in registration order.
func Executable(filePath string) *ExecutableCandidates {
	file, err := os.Open(filePath)
	if err != nil {
		log.Debug("Failed to open file: " + err.Error())
		return nil
	}
	defer fn.CloseReader(file, log.Warn)

	stat, err := file.Stat()
	if err != nil {
		log.Debug("Failed to stat file: " + err.Error())
		return nil
	}

	zipReader, err := zip.NewReader(file, stat.Size())
	if err != nil {
		log.Debug("Failed to read JAR file: " + err.Error())
		return nil
	}

	candidates := &ExecutableCandidates{
		Candidates: make([]*ExecutableEvidence, 0),
	}
	detectors := getExecutableDetectors()

	for _, detector := range detectors {
		result, err := detector.Detect(filePath, zipReader, file)
		if err != nil || result == nil {
			continue
		}
		candidates.Candidates = append(candidates.Candidates, result)
	}

	return candidates
}
