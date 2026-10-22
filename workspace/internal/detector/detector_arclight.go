package detector

import (
	"archive/zip"
	"bufio"
	"encoding/json/v2"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/input"
	versionpkg "github.com/mclucy/lucy/version"
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
func (d *arclightServerDetector) Detect(context DetectionContext, primaryFile *DetectionFile) (*RuntimeEvidence, error) {
	filePath := primaryFile.Path()
	zipReader, err := primaryFile.Archive()
	if err != nil {
		return nil, err
	}
	_ = context

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

	primary := catalog.VersionedRef{
		Ecosystem: catalog.EcosystemUnspecified,
		Name:      input.ToProjectName("arclight"),
		Version:   manifestSignals.implementationVersion,
	}
	components := []catalog.VersionedRef{
		primary,
		{
			Ecosystem: catalog.EcosystemMinecraft,
			Name:      input.ToProjectName("minecraft"),
			Version:   gameVersion,
		},
	}
	if loader, ok := arclightLoaderEcosystem(launchProps); ok {
		component := arclightLoaderComponent(loader)
		installerJSON, found, err := readArchiveEntry(
			zipReader,
			"META-INF/installer.json",
		)
		if err != nil {
			return nil, err
		}
		if found {
			if version, ok := parseArclightLoaderVersion(
				installerJSON,
				loader,
			); ok {
				component.Version = version
			}
		}
		components = append(components, component)
	}

	return &RuntimeEvidence{PrimaryPath: filePath, PrimaryRuntime: &primary, RuntimeComponents: components}, nil
}

type arclightManifestSignals struct {
	mainClass             string
	implementation        string
	implementationVersion versionpkg.Version
	gameVersion           versionpkg.Version
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
			signals.implementationVersion = versionpkg.Version(version)
			if parsedGameVersion := parseArclightGameVersionFromImplementation(version); hasConcreteVersion(parsedGameVersion) {
				signals.gameVersion = parsedGameVersion
			}
		}
	}
	return signals
}

func parseArclightGameVersionFromImplementation(version string) versionpkg.Version {
	if !strings.HasPrefix(version, "arclight-") {
		return versionpkg.Unknown
	}
	trimmed := strings.TrimPrefix(version, "arclight-")
	parts := strings.Split(trimmed, "-")
	if len(parts) == 0 || !isMinecraftReleaseVersion(parts[0]) {
		return versionpkg.Unknown
	}
	return versionpkg.Version(parts[0])
}

func parseArclightGameVersionFromPath(filePath string) versionpkg.Version {
	match := arclightJarNamePattern.FindStringSubmatch(filepath.Base(filePath))
	if match == nil {
		return versionpkg.Unknown
	}
	return versionpkg.Version(match[1])
}

func arclightLoaderEcosystem(launchProps []byte) (catalog.Ecosystem, bool) {
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
			return catalog.EcosystemFabric, true
		case "io.izzel.arclight.boot.neoforge.application.Main_Neoforge":
			return catalog.EcosystemNeoForge, true
		case "io.izzel.arclight.boot.forge.application.Main_Forge":
			return catalog.EcosystemForge, true
		default:
			return catalog.EcosystemUnspecified, false
		}
	}

	return catalog.EcosystemUnspecified, false
}

type arclightInstallerMetadata struct {
	Installer struct {
		Forge        string `json:"forge"`
		NeoForge     string `json:"neoforge"`
		FabricLoader string `json:"fabricLoader"`
	} `json:"installer"`
}

func arclightLoaderComponent(
	loader catalog.Ecosystem,
) catalog.VersionedRef {
	var name catalog.Name
	switch loader {
	case catalog.EcosystemFabric:
		name = "fabric-loader"
	case catalog.EcosystemNeoForge:
		name = "neoforge"
	case catalog.EcosystemForge:
		name = "forge"
	}
	return catalog.VersionedRef{
		Ecosystem: loader,
		Name:      name,
		Version:   versionpkg.Unknown,
	}
}

func parseArclightLoaderVersion(
	data []byte,
	loader catalog.Ecosystem,
) (versionpkg.Version, bool) {
	var metadata arclightInstallerMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return versionpkg.Unknown, false
	}

	var version string
	switch loader {
	case catalog.EcosystemFabric:
		version = metadata.Installer.FabricLoader
	case catalog.EcosystemNeoForge:
		version = metadata.Installer.NeoForge
	case catalog.EcosystemForge:
		version = metadata.Installer.Forge
	}

	loaderVersion := versionpkg.Version(strings.TrimSpace(version))
	if !hasConcreteVersion(loaderVersion) {
		return versionpkg.Unknown, false
	}
	return loaderVersion, true
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
