package detector

import (
	"archive/zip"
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mclucy/lucy/syntax"
	"github.com/mclucy/lucy/types"
)

var arclightJarNamePattern = regexp.MustCompile(
	`^arclight-(?:forge|neoforge|fabric)-(\d+\.\d+(?:\.\d+)?)(?:[-.].*)?\.jar$`,
)

type arclightServerDetector struct{}

func (d *arclightServerDetector) Name() string {
	return "arclight server"
}

// Sources:
// - https://arclight.izzel.io/
// - https://deepwiki.com/IzzelAliz/Arclight/1-overview
func (d *arclightServerDetector) Detect(
	filePath string,
	zipReader *zip.Reader,
	fileHandle *os.File,
) (*ExecutableEvidence, error) {
	manifest, ok, err := readArchiveEntry(zipReader, "META-INF/MANIFEST.MF")
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}

	manifestSignals := parseArclightManifest(manifest)
	if !manifestSignals.valid() {
		return nil, nil
	}

	hasLaunchProps, err := archiveContains(zipReader, "arclight-server-launch.properties")
	if err != nil {
		return nil, err
	}
	hasCommonJar, err := archiveContains(zipReader, "common.jar")
	if err != nil {
		return nil, err
	}
	if !hasLaunchProps && !hasCommonJar {
		return nil, nil
	}

	gameVersion := manifestSignals.gameVersion
	if !hasConcreteVersion(gameVersion) {
		gameVersion = parseArclightGameVersionFromPath(filePath)
	}

	return &ExecutableEvidence{
		PrimaryEntrance: filePath,
		GameVersion:     gameVersion,
		RuntimeIdentities: []types.PackageId{
			{
				Platform: types.PlatformAny,
				Name:     syntax.ToProjectName("arclight"),
				Version:  manifestSignals.loaderVersion,
			},
			{
				Platform: types.PlatformMinecraft,
				Name:     syntax.ToProjectName("minecraft"),
				Version:  gameVersion,
			},
		},
		Topology: &types.RuntimeTopology{
			PrimaryNode: "arclight",
			Nodes: []types.RuntimeNode{
				{
					ID:   "arclight",
					Role: types.RuntimeRoleHybrid,
					Capabilities: []types.RuntimeCapability{
						types.CapabilityForgeMods,
						types.CapabilityBukkitPlugins,
					},
				},
			},
		},
	}, nil
}

type arclightManifestSignals struct {
	mainClass      string
	implementation string
	mixinConnector string
	loaderVersion  types.RawVersion
	gameVersion    types.RawVersion
}

func (s arclightManifestSignals) valid() bool {
	return s.mainClass == "io.izzel.arclight.server.Launcher" &&
		s.implementation == "Arclight" &&
		s.mixinConnector == "io.izzel.arclight.common.mod.ArclightConnector"
}

func parseArclightManifest(data []byte) arclightManifestSignals {
	var signals arclightManifestSignals
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "Main-Class: "):
			signals.mainClass = strings.TrimSpace(strings.TrimPrefix(line, "Main-Class: "))
		case strings.HasPrefix(line, "Implementation-Title: "):
			signals.implementation = strings.TrimSpace(strings.TrimPrefix(line, "Implementation-Title: "))
		case strings.HasPrefix(line, "Implementation-Version: "):
			version := strings.TrimSpace(strings.TrimPrefix(line, "Implementation-Version: "))
			signals.loaderVersion = types.RawVersion(version)
			if parsedGameVersion := parseArclightGameVersionFromImplementation(version); hasConcreteVersion(parsedGameVersion) {
				signals.gameVersion = parsedGameVersion
			}
		case strings.HasPrefix(line, "MixinConnector: "):
			signals.mixinConnector = strings.TrimSpace(strings.TrimPrefix(line, "MixinConnector: "))
		}
	}
	return signals
}

func parseArclightGameVersionFromImplementation(version string) types.RawVersion {
	if !strings.HasPrefix(version, "arclight-") {
		return types.VersionUnknown
	}
	trimmed := strings.TrimPrefix(version, "arclight-")
	parts := strings.Split(trimmed, "-")
	if len(parts) == 0 || !isMinecraftReleaseVersion(parts[0]) {
		return types.VersionUnknown
	}
	return types.RawVersion(parts[0])
}

func parseArclightGameVersionFromPath(filePath string) types.RawVersion {
	match := arclightJarNamePattern.FindStringSubmatch(filepath.Base(filePath))
	if match == nil {
		return types.VersionUnknown
	}
	return types.RawVersion(match[1])
}

func archiveContains(zipReader *zip.Reader, name string) (bool, error) {
	_, ok, err := readArchiveEntry(zipReader, name)
	if err != nil {
		return false, err
	}
	return ok, nil
}

func init() {
	registerExecutableDetector(&arclightServerDetector{})
}
