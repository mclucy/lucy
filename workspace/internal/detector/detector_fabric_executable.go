package detector

import (
	"archive/zip"
	"bufio"
	"encoding/json/v2"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/internal/fileschema"
	"github.com/mclucy/lucy/internal/fn"
	"github.com/mclucy/lucy/log"
	versionpkg "github.com/mclucy/lucy/version"
)

const (
	fabricLaunchPropertiesPath = "fabric-server-launch.properties"
	// fabricLauncherPropertiesPath is the sidecar file FabricServerLauncher
	// keeps beside the shim; note the extra "r" versus
	// fabricLaunchPropertiesPath, which sits inside the jar.
	fabricLauncherPropertiesPath = "fabric-server-launcher.properties"
	fabricServerJarProperty      = "serverJar"
	fabricDefaultServerJar       = "server.jar"
	fabricModernKnotServer       = "net.fabricmc.loader.impl.launch.knot.KnotServer"
	fabricLegacyKnotServer       = "net.fabricmc.loader.launch.knot.KnotServer"
)

// fabricServerSingleFileDetector detects Fabric single-file servers.
type fabricServerSingleFileDetector struct{}

func (d *fabricServerSingleFileDetector) Name() string {
	return "fabric server"
}

func (d *fabricServerSingleFileDetector) Detect(context DetectionContext, primaryFile *DetectionFile) (exec *RuntimeEvidence, err error) {
	filePath := primaryFile.Path()
	zipReader, err := primaryFile.Archive()
	if err != nil {
		return nil, err
	}
	_ = context

	loaderVersion := versionpkg.Unknown
	gameVersion := versionpkg.Unknown
	for _, f := range zipReader.File {
		if f.Name == "install.properties" {
			r, err := f.Open()
			if err != nil {
				continue
			}
			defer fn.CloseReader(r, log.Warn)

			scanner := bufio.NewScanner(r)
			for scanner.Scan() {
				line := scanner.Text()
				if after, found := strings.CutPrefix(
					line,
					"fabric-loader-version=",
				); found {
					loaderVersion = versionpkg.Version(after)
				} else if after, found := strings.CutPrefix(
					line,
					"game-version=",
				); found {
					gameVersion = versionpkg.Version(after)
				}
			}
			if scanner.Err() != nil {
				continue
			}
			if loaderVersion == versionpkg.Unknown || gameVersion == versionpkg.Unknown {
				continue
			}
			break
		}
	}

	if loaderVersion == versionpkg.Unknown || gameVersion == versionpkg.Unknown {
		return nil, nil
	}

	return newFabricRuntimeEvidence(
		filePath,
		loaderVersion,
		gameVersion,
	), nil
}

// fabricServerLauncherDetector detects Fabric server launchers.
type fabricServerLauncherDetector struct{}

func (d *fabricServerLauncherDetector) Name() string {
	return "fabric server"
}

func (d *fabricServerLauncherDetector) Detect(context DetectionContext, primaryFile *DetectionFile) (exec *RuntimeEvidence, err error) {
	filePath := primaryFile.Path()
	zipReader, err := primaryFile.Archive()
	if err != nil {
		return nil, err
	}
	_ = context

	data, ok, err := readArchiveEntry(zipReader, fabricLaunchPropertiesPath)
	if err != nil || !ok {
		return nil, err
	}
	if !fabricLauncherPropertiesAreServer(data) {
		return nil, nil
	}

	loaderVersion, gameVersion := parseFabricLauncherManifestVersions(zipReader)
	if loaderVersion == versionpkg.Unknown {
		loaderVersion = parseFabricLauncherBundledLoaderVersion(zipReader)
	}
	if gameVersion == versionpkg.Unknown {
		gameVersion = parseFabricLauncherSidecarGameVersion(context, primaryFile)
	}
	// Installer 1.1+ shims embed no version evidence, so the last resort is
	// the vanilla jar the shim boots: FabricServerLauncher reads serverJar
	// from fabric-server-launcher.properties and defaults to server.jar.
	launcherProperties := []byte(nil)
	if propertiesFile, ok := context.Sibling(primaryFile, "fabric-server-launcher.properties"); ok {
		launcherProperties, _ = propertiesFile.Read()
	}
	serverJarPath := fabricLauncherServerJarPath(primaryFile, launcherProperties)
	if gameVersion == versionpkg.Unknown {
		if serverJar, ok := context.RootFile(serverJarPath); ok {
			gameVersion = parseServerJarGameVersion(serverJar)
		}
	}
	if loaderVersion == versionpkg.Unknown || gameVersion == versionpkg.Unknown {
		return nil, nil
	}

	exec = newFabricRuntimeEvidence(filePath, loaderVersion, gameVersion)
	// The shim boots this jar; the probe must not report it as a standalone
	// runtime beside the shim.
	if serverJar, ok := context.RootFile(serverJarPath); ok {
		exec.ConsumedFiles = []*DetectionFile{serverJar}
	}
	return exec, nil
}

func fabricLauncherPropertiesAreServer(data []byte) bool {
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		if after, found := strings.CutPrefix(line, "launch.mainClass="); found {
			return after == fabricModernKnotServer || after == fabricLegacyKnotServer
		}
	}

	return false
}

func parseFabricLauncherManifestVersions(zipReader *zip.Reader) (
	loaderVersion versionpkg.Version,
	gameVersion versionpkg.Version,
) {
	loaderVersion = versionpkg.Unknown
	gameVersion = versionpkg.Unknown
	for _, f := range zipReader.File {
		if f.Name == "META-INF/MANIFEST.MF" {
			r, err := f.Open()
			if err != nil {
				continue
			}
			defer fn.CloseReader(r, log.Warn)

			var classPaths []string
			s := bufio.NewScanner(r)
			for s.Scan() {
				line := s.Text()
				if after, found := strings.CutPrefix(
					line,
					"Class-Path: ",
				); found {
					var classPathsBuilder strings.Builder
					classPathsBuilder.WriteString(after)
					for s.Scan() && strings.HasPrefix(s.Text(), " ") {
						line := strings.TrimSpace(s.Text())
						classPathsBuilder.WriteString(line)
					}
					classPaths = strings.Split(classPathsBuilder.String(), " ")
				}
			}
			if s.Err() != nil {
				return versionpkg.Unknown, versionpkg.Unknown
			}

			for _, path := range classPaths {
				if after, found := strings.CutPrefix(
					path,
					"libraries/net/fabricmc/fabric-loader/",
				); found {
					loaderVersion = versionpkg.Version(
						strings.Split(after, "/")[0],
					)
				} else if after, found := strings.CutPrefix(
					path,
					"libraries/net/fabricmc/intermediary/",
				); found {
					gameVersion = versionpkg.Version(
						strings.Split(after, "/")[0],
					)
				}
			}

			return loaderVersion, gameVersion
		}
	}

	return loaderVersion, gameVersion
}

func parseFabricLauncherBundledLoaderVersion(zipReader *zip.Reader) versionpkg.Version {
	data, ok, err := readArchiveEntry(zipReader, "fabric.mod.json")
	if err != nil || !ok {
		return versionpkg.Unknown
	}

	var metadata fileschema.FileFabricModIdentifier
	if err := json.Unmarshal(data, &metadata); err != nil {
		return versionpkg.Unknown
	}
	if metadata.ID != "fabricloader" || strings.TrimSpace(metadata.Version) == "" {
		return versionpkg.Unknown
	}

	return versionpkg.Version(metadata.Version)
}

func parseFabricLauncherSidecarGameVersion(context DetectionContext, primaryFile *DetectionFile) versionpkg.Version {
	sidecar, ok := context.Sibling(primaryFile, "version.json")
	if !ok {
		return versionpkg.Unknown
	}
	data, err := sidecar.Read()
	if err != nil {
		return versionpkg.Unknown
	}

	var metadata struct {
		Name          string `json:"name"`
		ReleaseTarget string `json:"release_target"`
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		return versionpkg.Unknown
	}
	if strings.TrimSpace(metadata.Name) != "" {
		return versionpkg.Version(metadata.Name)
	}
	if strings.TrimSpace(metadata.ReleaseTarget) != "" {
		return versionpkg.Version(metadata.ReleaseTarget)
	}

	return versionpkg.Unknown
}

// parseServerJarGameVersion reads the game version from the version.json of a
// server jar on disk. Any problem yields VersionUnknown: this source is the
// last resort for launch shims with no embedded version evidence.
func parseServerJarGameVersion(file *DetectionFile) versionpkg.Version {
	zipReader, err := file.Archive()
	if err != nil {
		return versionpkg.Unknown
	}
	data, ok, err := readArchiveEntry(zipReader, mojangVersionJSONEntry)
	if err != nil || !ok {
		return versionpkg.Unknown
	}
	version, guarded, err := mojangVersionFromJSON(data)
	if err != nil || guarded || version == "" || version == versionpkg.Unknown {
		return versionpkg.Unknown
	}
	return version
}

// fabricLauncherServerJarPath returns the server jar that the launch shim at
// shimPath boots. FabricServerLauncher reads serverJar from
// fabric-server-launcher.properties beside the shim and falls back to
// server.jar when the file or the entry is missing.
func fabricLauncherServerJarPath(primaryFile *DetectionFile, properties []byte) string {
	name := fabricDefaultServerJar
	if value := propertiesValue(properties, fabricServerJarProperty); value != "" {
		name = value
	}
	if filepath.IsAbs(name) {
		return filepath.Clean(name)
	}
	return filepath.Join(filepath.Dir(primaryFile.Path()), name)
}

// propertiesValue returns the value of key in a Java properties payload, or
// "" when the key is absent.
func propertiesValue(data []byte, key string) string {
	var value string
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}

		separator := strings.IndexAny(line, "=:\t ")
		if separator < 0 {
			continue
		}
		propertyKey := strings.TrimSpace(line[:separator])
		if propertyKey != key {
			continue
		}
		propertyValue := strings.TrimLeft(line[separator:], "=:\t ")
		value = strings.TrimSpace(propertyValue)
	}
	return value
}

func newFabricRuntimeEvidence(
	filePath string,
	loaderVersion versionpkg.Version,
	gameVersion versionpkg.Version,
) *RuntimeEvidence {
	return &RuntimeEvidence{PrimaryPath: filePath, PrimaryRuntime: &catalog.VersionedRef{
		Ecosystem: catalog.EcosystemFabric,
		Name:      "fabric",
		Version:   loaderVersion,
	}, RuntimeComponents: []catalog.VersionedRef{
		{
			Ecosystem: catalog.EcosystemFabric,
			Name:      "fabric-loader",
			Version:   loaderVersion,
		},
		{
			Ecosystem: catalog.EcosystemMinecraft,
			Name:      "minecraft",
			Version:   gameVersion,
		},
	}}
}

func init() {
	registerExecutableDetector(&fabricServerSingleFileDetector{})
	registerExecutableDetector(&fabricServerLauncherDetector{})
}
