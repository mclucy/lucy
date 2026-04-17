package detector

import (
	"archive/zip"
	"bufio"
	"encoding/json"
	"io"
	"os"
	"strings"
	"sync"

	externaltype "github.com/mclucy/lucy/exttype"
	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/tools"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream/slugresolve"
)

// fabricServerSingleFileDetector detects Fabric single-file servers
// This is one of the two methods of Fabric installation. One larger .jar file
// it placed at the root of the server directory. It handles the initialization
// and the downloading of the required libraries and minecraft version.
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
					loaderVersion = types.RawVersion(after)
				} else if after, found := strings.CutPrefix(
					line,
					"game-version=",
				); found {
					gameVersion = types.RawVersion(after)
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
		RuntimeIdentities: []types.PackageId{
			{
				Platform: types.PlatformFabric,
				Name:     "fabric",
				Version:  loaderVersion,
			},
			{
				Platform: types.PlatformMinecraft,
				Name:     "minecraft",
				Version:  gameVersion,
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

// fabricServerLauncherDetector detects Fabric server launchers
// This is one of the two methods of Fabric installation. A lightweight
// launcher .jar file is placed at the root of the server directory. It only
// records the paths to the required libraries.
//
// The detection process is rather complicated.
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

			// Here we just parse the paths to find the versions.
			//
			// Although been seemingly unreliable, this is a justified method.
			// The lightweight launcher .jar's idea is to not restrictively
			// specify anything but only the paths to the libraries(classes).
			// Besides, it is the user's responsibility to ensure the presence
			// of the required libraries.
			for _, path := range classPaths {
				if after, found := strings.CutPrefix(
					path,
					"libraries/net/fabricmc/fabric-loader/",
				); found {
					loaderVersion = types.RawVersion(
						strings.Split(after, "/")[0],
					)
				} else if after, found := strings.CutPrefix(
					path,
					"libraries/net/fabricmc/intermediary/",
				); found {
					gameVersion = types.RawVersion(
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
				RuntimeIdentities: []types.PackageId{
					{
						Platform: types.PlatformFabric,
						Name:     "fabric",
						Version:  loaderVersion,
					},
					{
						Platform: types.PlatformMinecraft,
						Name:     "minecraft",
						Version:  gameVersion,
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

// fabricModDetector detects Fabric mods in JAR files
type fabricModDetector struct{}

func (d *fabricModDetector) Name() string {
	return "fabric mod"
}

func (d *fabricModDetector) Detect(
	zipReader *zip.Reader,
	fileHandle *os.File,
) (packages []types.Package, err error) {
	var wg sync.WaitGroup
	for _, f := range zipReader.File {
		if f.Name == "fabric.mod.json" {
			r, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer tools.CloseReader(r, logger.Warn)

			data, err := io.ReadAll(r)
			if err != nil {
				return nil, err
			}

			modInfo := &externaltype.FileFabricModIdentifier{}
			err = json.Unmarshal(data, modInfo)
			if err != nil {
				return nil, err
			}

			pkg := translateFabricMod(modInfo, fileHandle.Name())

			packages = append(packages, pkg)

			// Pre-populate slugmap for both sources using metadata URLs and file hash
			var metaURLs []string
			for _, key := range []string{"homepage", "sources", "issues"} {
				if u := modInfo.Contact[key]; u != "" {
					metaURLs = append(metaURLs, u)
				}
			}
			wg.Add(2)
			go func(name, path string, urls []string) {
				defer wg.Done()
				slugresolve.ResolveSlug(types.SourceModrinth, name, path, urls)
			}(string(pkg.Id.Name), fileHandle.Name(), metaURLs)
			go func(name, path string, urls []string) {
				defer wg.Done()
				slugresolve.ResolveSlug(types.SourceCurseForge, name, path, urls)
			}(string(pkg.Id.Name), fileHandle.Name(), metaURLs)
		}
	}

	wg.Wait()
	return packages, nil
}

func init() {
	registerExecutableDetector(&fabricServerSingleFileDetector{})
	registerExecutableDetector(&fabricServerLauncherDetector{})
	registerModDetector(&fabricModDetector{})
}
