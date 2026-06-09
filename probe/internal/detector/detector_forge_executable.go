package detector

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
)

type forgeLegacyDetector struct{}

func (d *forgeLegacyDetector) Name() string {
	return "forge legacy server"
}

// Sources:
// - https://docs.minecraftforge.net/en/1.16.x/gettingstarted/
// - https://forums.minecraftforge.net/topic/102544-forge-370-minecraft-1171/
func (d *forgeLegacyDetector) Detect(
	filePath string,
	zipReader *zip.Reader,
	fileHandle *os.File,
) (*ExecutableEvidence, error) {
	base := filepath.Base(filePath)
	if !strings.Contains(base, "forge-") || !strings.Contains(
		base,
		"universal",
	) {
		return nil, nil
	}

	forgeVersion, gameVersion := parseForgeManifest(zipReader)
	if !hasConcreteVersion(forgeVersion) {
		return nil, nil
	}
	if !hasConcreteVersion(gameVersion) {
		gameVersion, _, _ = parseForgeVersionTupleFromPath(filePath)
	}
	if !hasConcreteVersion(gameVersion) {
		return nil, nil
	}

	return buildForgeRuntimeInfo(filePath, gameVersion, forgeVersion), nil
}

type forgeModernDetector struct{}

func (d *forgeModernDetector) Name() string {
	return "forge modern server"
}

// Sources:
// - https://docs.minecraftforge.net/en/latest/gettingstarted/server/
// - https://forums.minecraftforge.net/topic/102544-forge-370-minecraft-1171/
func (d *forgeModernDetector) Detect(
	filePath string,
	zipReader *zip.Reader,
	fileHandle *os.File,
) (*ExecutableEvidence, error) {
	gameVersion, forgeVersion, ok := parseForgeVersionTupleFromPath(filePath)
	if !ok || compareForgeMajor(forgeVersion, 61) >= 0 {
		return nil, nil
	}

	base := filepath.Base(filePath)
	if !strings.HasPrefix(base, "forge-") {
		return nil, nil
	}
	if !strings.Contains(base, "-server") && !strings.Contains(
		base,
		"-universal",
	) {
		return nil, nil
	}
	if !forgeHasSibling(filePath, "unix_args.txt", "win_args.txt") {
		return nil, nil
	}

	return buildForgeRuntimeInfo(filePath, gameVersion, forgeVersion), nil
}

type forgeLatestDetector struct{}

func (d *forgeLatestDetector) Name() string {
	return "forge latest server"
}

// Sources:
// - https://docs.minecraftforge.net/en/latest/gettingstarted/server/
// - https://forums.minecraftforge.net/topic/154652-how-to-install-forge-6110-for-1211-server/
func (d *forgeLatestDetector) Detect(
	filePath string,
	zipReader *zip.Reader,
	fileHandle *os.File,
) (*ExecutableEvidence, error) {
	gameVersion, forgeVersion, ok := parseForgeVersionTupleFromPath(filePath)
	if !ok || compareForgeMajor(forgeVersion, 61) < 0 {
		return nil, nil
	}

	base := filepath.Base(filePath)
	if !strings.HasPrefix(base, "forge-") || !strings.Contains(
		base,
		"-server",
	) {
		return nil, nil
	}
	if !forgeHasSibling(
		filePath,
		"unix_args.txt",
		"win_args.txt",
		strings.Replace(base, "-server.jar", "-shim.jar", 1),
		strings.Replace(base, "-server.jar", "-universal.jar", 1),
	) {
		return nil, nil
	}

	return buildForgeRuntimeInfo(filePath, gameVersion, forgeVersion), nil
}

// forgeServerDetector detects Forge servers via manifest metadata fallback.
type forgeServerDetector struct{}

func (d *forgeServerDetector) Name() string {
	return "forge server"
}

// Sources:
// - https://docs.minecraftforge.net/en/latest/gettingstarted/server/
// - https://docs.minecraftforge.net/en/1.16.x/gettingstarted/
func (d *forgeServerDetector) Detect(
	filePath string,
	zipReader *zip.Reader,
	fileHandle *os.File,
) (*ExecutableEvidence, error) {
	forgeVersion, gameVersion := parseForgeManifest(zipReader)
	if !hasConcreteVersion(forgeVersion) {
		return nil, nil
	}
	if !hasConcreteVersion(gameVersion) {
		parsedGameVersion, _, ok := parseForgeVersionTupleFromPath(filePath)
		if ok {
			gameVersion = parsedGameVersion
		}
	}
	if !hasConcreteVersion(gameVersion) {
		return nil, nil
	}

	return buildForgeRuntimeInfo(filePath, gameVersion, forgeVersion), nil
}

func init() {
	registerExecutableDetector(&forgeLegacyDetector{})
	registerExecutableDetector(&forgeModernDetector{})
	registerExecutableDetector(&forgeLatestDetector{})
	registerExecutableDetector(&forgeServerDetector{})
}
