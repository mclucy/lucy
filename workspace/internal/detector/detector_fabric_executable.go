package detector

import (
	"archive/zip"
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/internal/fileschema"
	"github.com/mclucy/lucy/internal/fn"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/types"
)

const (
	fabricLaunchPropertiesPath = "fabric-server-launch.properties"
	fabricModernKnotServer     = "net.fabricmc.loader.impl.launch.knot.KnotServer"
	fabricLegacyKnotServer     = "net.fabricmc.loader.launch.knot.KnotServer"
)

// fabricServerSingleFileDetector detects Fabric single-file servers.
type fabricServerSingleFileDetector struct{}

func (d *fabricServerSingleFileDetector) Name() string {
	return "fabric server"
}

func (d *fabricServerSingleFileDetector) Detect(
	filePath string,
	zipReader *zip.Reader,
	fileHandle *os.File,
) (exec *ExecutableEvidence, err error) {
	loaderVersion := types.VersionUnknown
	gameVersion := types.VersionUnknown
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
					loaderVersion = types.BareVersion(after)
				} else if after, found := strings.CutPrefix(
					line,
					"game-version=",
				); found {
					gameVersion = types.BareVersion(after)
				}
			}
			if scanner.Err() != nil {
				continue
			}
			if loaderVersion == types.VersionUnknown || gameVersion == types.VersionUnknown {
				continue
			}
			break
		}
	}

	if loaderVersion == types.VersionUnknown || gameVersion == types.VersionUnknown {
		return nil, nil
	}

	return newFabricExecutableEvidence(
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

func (d *fabricServerLauncherDetector) Detect(
	filePath string,
	zipReader *zip.Reader,
	fileHandle *os.File,
) (exec *ExecutableEvidence, err error) {
	data, ok, err := readArchiveEntry(zipReader, fabricLaunchPropertiesPath)
	if err != nil || !ok {
		return nil, err
	}
	if !fabricLauncherPropertiesAreServer(data) {
		return nil, nil
	}

	loaderVersion, gameVersion := parseFabricLauncherManifestVersions(zipReader)
	if loaderVersion == types.VersionUnknown {
		loaderVersion = parseFabricLauncherBundledLoaderVersion(zipReader)
	}
	if gameVersion == types.VersionUnknown {
		gameVersion = parseFabricLauncherSidecarGameVersion(filePath)
	}
	if loaderVersion == types.VersionUnknown || gameVersion == types.VersionUnknown {
		return nil, nil
	}

	return newFabricExecutableEvidence(
		filePath,
		loaderVersion,
		gameVersion,
	), nil
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
	loaderVersion types.BareVersion,
	gameVersion types.BareVersion,
) {
	loaderVersion = types.VersionUnknown
	gameVersion = types.VersionUnknown
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
				return types.VersionUnknown, types.VersionUnknown
			}

			for _, path := range classPaths {
				if after, found := strings.CutPrefix(
					path,
					"libraries/net/fabricmc/fabric-loader/",
				); found {
					loaderVersion = types.BareVersion(
						strings.Split(after, "/")[0],
					)
				} else if after, found := strings.CutPrefix(
					path,
					"libraries/net/fabricmc/intermediary/",
				); found {
					gameVersion = types.BareVersion(
						strings.Split(after, "/")[0],
					)
				}
			}

			return loaderVersion, gameVersion
		}
	}

	return loaderVersion, gameVersion
}

func parseFabricLauncherBundledLoaderVersion(zipReader *zip.Reader) types.BareVersion {
	data, ok, err := readArchiveEntry(zipReader, "fabric.mod.json")
	if err != nil || !ok {
		return types.VersionUnknown
	}

	var metadata fileschema.FileFabricModIdentifier
	if err := json.Unmarshal(data, &metadata); err != nil {
		return types.VersionUnknown
	}
	if metadata.Id != "fabricloader" || strings.TrimSpace(metadata.Version) == "" {
		return types.VersionUnknown
	}

	return types.BareVersion(metadata.Version)
}

func parseFabricLauncherSidecarGameVersion(filePath string) types.BareVersion {
	data, err := os.ReadFile(
		filepath.Join(
			filepath.Dir(filePath),
			"version.json",
		),
	)
	if err != nil {
		return types.VersionUnknown
	}

	var metadata struct {
		Name          string `json:"name"`
		ReleaseTarget string `json:"release_target"`
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		return types.VersionUnknown
	}
	if strings.TrimSpace(metadata.Name) != "" {
		return types.BareVersion(metadata.Name)
	}
	if strings.TrimSpace(metadata.ReleaseTarget) != "" {
		return types.BareVersion(metadata.ReleaseTarget)
	}

	return types.VersionUnknown
}

func newFabricExecutableEvidence(
	filePath string,
	loaderVersion types.BareVersion,
	gameVersion types.BareVersion,
) *ExecutableEvidence {
	return &ExecutableEvidence{
		PrimaryEntrance: filePath,
		GameVersion:     gameVersion,
		RuntimeIdentities: []types.VersionedPackageRef{
			{
				PackageRef: types.PackageRef{
					Eco:  types.EcoFabric,
					Name: "fabric",
				},
				Version: loaderVersion,
			},
			{
				PackageRef: types.PackageRef{
					Eco:  types.EcoMinecraft,
					Name: "minecraft",
				},
				Version: gameVersion,
			},
		},
		Topology: &types.ServerTopology{
			PrimaryNode: "fabric",
			Nodes: []types.RuntimeNode{
				{
					ID:           "fabric",
					Role:         types.RuntimeRoleModLoader,
					Capabilities: []types.RuntimeCapability{types.CapabilityFabricLoader},
				},
			},
		},
	}
}

func init() {
	registerExecutableDetector(&fabricServerSingleFileDetector{})
	registerExecutableDetector(&fabricServerLauncherDetector{})
}
