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

var UnknownExecutable = &types.ExecutableInfo{
	Path:          "",
	GameVersion:   types.UnknownVersion,
	ModLoader:     types.UnknownPlatform,
	LoaderVersion: types.UnknownVersion,
	BootCommand:   nil,
}

// Executable analyzes a JAR file using all registered detectors
// and returns the first successful match (in registration order).
// If multiple detectors match, callers should handle ambiguity separately.
func Executable(filePath string) *types.ExecutableInfo {
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

	var candidates []*types.ExecutableInfo
	detectors := getExecutableDetectors()

	for _, detector := range detectors {
		result, err := detector.Detect(filePath, zipReader, file)
		if err != nil || result == nil {
			continue
		}
		candidates = append(candidates, result)
	}

	if len(candidates) == 0 {
		return nil
	}

	if len(candidates) > 1 {
		// TODO: Modify this by need to handle multiple matches better
		logger.Warn(fmt.Errorf("multiple executable detectors matched; marking as unknown"))
		return UnknownExecutable
	}

	return candidates[0]
}

// Packages analyzes a mod/plugin file
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
	case ".pyz", ".mcdr":
		McdrPlugin(filePath)
	default:
		return nil
	}

	return
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
