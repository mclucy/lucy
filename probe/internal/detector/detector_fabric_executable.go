package detector

import (
	"archive/zip"
	"bufio"
	"os"
	"strings"

	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/tools"
	"github.com/mclucy/lucy/types"
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
			defer tools.CloseReader(r, logger.Warn)

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
			if loaderVersion == types.VersionUnknown || gameVersion == types.VersionUnknown {
				continue
			}
			break
		}
	}

	if loaderVersion == types.VersionUnknown || gameVersion == types.VersionUnknown {
		return nil, nil
	}

	exec = &ExecutableEvidence{
		PrimaryEntrance: filePath,
		GameVersion:     gameVersion,
		RuntimeIdentities: []types.VersionedPackageRef{
			{
				PackageRef: types.PackageRef{
					Platform: types.PlatformFabric,
					Name:     "fabric",
				},
				Version: loaderVersion,
			},
			{
				PackageRef: types.PackageRef{
					Platform: types.PlatformMinecraft,
					Name:     "minecraft",
				},
				Version: gameVersion,
			},
		},
		Topology: &types.RuntimeTopology{
			PrimaryNode: "fabric",
			Nodes: []types.RuntimeNode{
				{
					ID:           "fabric",
					Role:         types.RuntimeRoleModLoader,
					Capabilities: []types.RuntimeCapability{types.CapabilityFabricMods},
				},
			},
		},
	}

	return exec, nil
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
	var valid bool
	for _, f := range zipReader.File {
		if f.Name == "fabric-server-launch.properties" {
			r, err := f.Open()
			if err != nil {
				continue
			}
			defer tools.CloseReader(r, logger.Warn)

			scanner := bufio.NewScanner(r)
			for scanner.Scan() {
				line := scanner.Text()
				if line == "launch.mainClass=net.fabricmc.loader.impl.launch.knot.KnotServer" {
					valid = true
					break
				}
			}
		}
	}

	if !valid {
		return nil, nil
	}

	loaderVersion := types.VersionUnknown
	gameVersion := types.VersionUnknown
	for _, f := range zipReader.File {
		if f.Name == "META-INF/MANIFEST.MF" {
			r, err := f.Open()
			if err != nil {
				continue
			}
			defer tools.CloseReader(r, logger.Warn)

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
					for s.Scan() && !strings.Contains(s.Text(), ":") {
						line := s.Text()
						line = strings.TrimSpace(line)
						classPathsBuilder.WriteString(line)
					}
					classPaths = strings.Split(classPathsBuilder.String(), " ")
				}
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

			if loaderVersion == types.VersionUnknown || gameVersion == types.VersionUnknown {
				continue
			}

			exec = &ExecutableEvidence{
				PrimaryEntrance: filePath,
				GameVersion:     gameVersion,
				RuntimeIdentities: []types.VersionedPackageRef{
					{
						PackageRef: types.PackageRef{
							Platform: types.PlatformFabric,
							Name:     "fabric",
						},
						Version: loaderVersion,
					},
					{
						PackageRef: types.PackageRef{
							Platform: types.PlatformMinecraft,
							Name:     "minecraft",
						},
						Version: gameVersion,
					},
				},
				Topology: &types.RuntimeTopology{
					PrimaryNode: "fabric",
					Nodes: []types.RuntimeNode{
						{
							ID:           "fabric",
							Role:         types.RuntimeRoleModLoader,
							Capabilities: []types.RuntimeCapability{types.CapabilityFabricMods},
						},
					},
				},
			}

			return exec, nil
		}
	}

	return nil, nil
}

func init() {
	registerExecutableDetector(&fabricServerSingleFileDetector{})
	registerExecutableDetector(&fabricServerLauncherDetector{})
}
