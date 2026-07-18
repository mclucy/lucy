package detector

import (
	"archive/zip"
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mclucy/lucy/input"
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

	launchProps, hasLaunchProps, err := readArchiveEntry(
		zipReader,
		"arclight-server-launch.properties",
	)
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

	primary := types.VersionedPackageRef{
		PackageRef: types.PackageRef{
			Eco:  types.EcoUnspecified,
			Name: input.ToProjectName("arclight"),
		},
		Version: manifestSignals.implementationVersion,
	}
	components := []types.VersionedPackageRef{
		primary,
		{
			PackageRef: types.PackageRef{
				Eco:  types.EcoMinecraft,
				Name: input.ToProjectName("minecraft"),
			},
			Version: gameVersion,
		},
	}
	if loader, ok := arclightLoaderEcosystem(launchProps); ok {
		installerJSON, found, err := readArchiveEntry(
			zipReader,
			"META-INF/installer.json",
		)
		if err != nil {
			return nil, err
		}
		if found {
			if component, ok := parseArclightLoaderComponent(
				installerJSON,
				loader,
			); ok {
				components = append(components, component)
			}
		}
	}

	return &ExecutableEvidence{PrimaryPath: filePath, PrimaryRuntime: &primary, RuntimeComponents: components}, nil
}

type arclightManifestSignals struct {
	mainClass             string
	implementation        string
	implementationVersion types.BareVersion
	gameVersion           types.BareVersion
}

func (s arclightManifestSignals) valid() bool {
	return s.mainClass == "io.izzel.arclight.server.Launcher" &&
		s.implementation == "Arclight"
}

func parseArclightManifest(data []byte) arclightManifestSignals {
	var signals arclightManifestSignals
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "Main-Class: "):
			signals.mainClass = strings.TrimSpace(
				strings.TrimPrefix(
					line,
					"Main-Class: ",
				),
			)
		case strings.HasPrefix(line, "Implementation-Title: "):
			signals.implementation = strings.TrimSpace(
				strings.TrimPrefix(
					line,
					"Implementation-Title: ",
				),
			)
		case strings.HasPrefix(line, "Implementation-Version: "):
			version := strings.TrimSpace(
				strings.TrimPrefix(
					line,
					"Implementation-Version: ",
				),
			)
			signals.implementationVersion = types.BareVersion(version)
			if parsedGameVersion := parseArclightGameVersionFromImplementation(version); hasConcreteVersion(parsedGameVersion) {
				signals.gameVersion = parsedGameVersion
			}
		}
	}
	return signals
}

func parseArclightGameVersionFromImplementation(version string) types.BareVersion {
	if !strings.HasPrefix(version, "arclight-") {
		return types.VersionUnknown
	}
	trimmed := strings.TrimPrefix(version, "arclight-")
	parts := strings.Split(trimmed, "-")
	if len(parts) == 0 || !isMinecraftReleaseVersion(parts[0]) {
		return types.VersionUnknown
	}
	return types.BareVersion(parts[0])
}

func parseArclightGameVersionFromPath(filePath string) types.BareVersion {
	match := arclightJarNamePattern.FindStringSubmatch(filepath.Base(filePath))
	if match == nil {
		return types.VersionUnknown
	}
	return types.BareVersion(match[1])
}

func arclightLoaderEcosystem(launchProps []byte) (types.Ecosystem, bool) {
	scanner := bufio.NewScanner(strings.NewReader(string(launchProps)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != "launch.mainClass" {
			continue
		}
		switch strings.TrimSpace(value) {
		case "io.izzel.arclight.boot.fabric.application.Main_Fabric":
			return types.EcoFabric, true
		case "io.izzel.arclight.boot.neoforge.application.Main_Neoforge":
			return types.EcoNeoforge, true
		case "io.izzel.arclight.boot.forge.application.Main_Forge":
			return types.EcoForge, true
		default:
			return types.EcoUnspecified, false
		}
	}

	return types.EcoUnspecified, false
}

type arclightInstallerMetadata struct {
	Installer struct {
		Forge        string `json:"forge"`
		NeoForge     string `json:"neoforge"`
		FabricLoader string `json:"fabricLoader"`
	} `json:"installer"`
}

func parseArclightLoaderComponent(
	data []byte,
	loader types.Ecosystem,
) (types.VersionedPackageRef, bool) {
	var metadata arclightInstallerMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return types.VersionedPackageRef{}, false
	}

	var name types.BarePackageName
	var version string
	switch loader {
	case types.EcoFabric:
		name = "fabric-loader"
		version = metadata.Installer.FabricLoader
	case types.EcoNeoforge:
		name = "neoforge"
		version = metadata.Installer.NeoForge
	case types.EcoForge:
		name = "forge"
		version = metadata.Installer.Forge
	default:
		return types.VersionedPackageRef{}, false
	}

	loaderVersion := types.BareVersion(strings.TrimSpace(version))
	if !hasConcreteVersion(loaderVersion) {
		return types.VersionedPackageRef{}, false
	}
	return types.VersionedPackageRef{
		PackageRef: types.PackageRef{
			Eco:  loader,
			Name: name,
		},
		Version: loaderVersion,
	}, true
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
