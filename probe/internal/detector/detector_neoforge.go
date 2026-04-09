package detector

import (
	"archive/zip"
	"bufio"
	"io"
	"os"
	"strings"

	externaltype "github.com/mclucy/lucy/exttype"
	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/syntax"
	"github.com/mclucy/lucy/tools"
	"github.com/mclucy/lucy/types"

	"github.com/pelletier/go-toml"
)

// neoforgeServerDetector detects NeoForge servers
type neoforgeServerDetector struct{}

func (d *neoforgeServerDetector) Name() string {
	return "neoforge server"
}

func (d *neoforgeServerDetector) Detect(
filePath string,
zipReader *zip.Reader,
fileHandle *os.File,
) (*types.RuntimeInfo, error) {
	neoforgeLoaderVersion := types.VersionUnknown
	gameVersion := types.VersionUnknown

	// Single pass through manifest for both game version and classpath
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

				// Parse game version from manifest
				if line == "Specification-Title: Minecraft" {
					// the n+2 line contains the version
					if !s.Scan() {
						continue
					}
					if !s.Scan() {
						continue
					}
					line := s.Text()
					if after, found := strings.CutPrefix(
						line,
						"Specification-Version: ",
					); found {
						gameVersion = types.RawVersion(after)
					}
				}

				// Parse Class-Path for NeoForge classpath entry
				if after, found := strings.CutPrefix(
					line,
					"Class-Path: ",
				); found {
					classPathsStr := after
					for s.Scan() && !strings.Contains(s.Text(), ":") {
						line := s.Text()
						line = strings.TrimSpace(line)
						classPathsStr += line
					}
					classPaths = strings.Split(classPathsStr, " ")
				}
			}

			// Primary detection signal: NeoForge classpath entry
			for _, path := range classPaths {
				if after, found := strings.CutPrefix(
					path,
					"libraries/net/neoforged/neoforge/",
				); found {
					neoforgeLoaderVersion = types.RawVersion(
						strings.Split(after, "/")[0],
					)
					break
				}
			}

			break
		}
	}

	// Return nil if primary NeoForge signal not found
	if neoforgeLoaderVersion == types.VersionUnknown {
		return nil, nil
	}

	// Build and return result (gameVersion may be VersionUnknown if not in manifest)
	exec := &types.RuntimeInfo{
		PrimaryEntrance: filePath,
		GameVersion:     gameVersion,
		BootCommand:     nil,
		RuntimeIdentities: []types.PackageId{
			{
				Platform: types.PlatformNeoforge,
				Name:     syntax.ToProjectName("neoforge"),
				Version:  neoforgeLoaderVersion,
			},
			{
				Platform: types.PlatformMinecraft,
				Name:     syntax.ToProjectName("minecraft"),
				Version:  gameVersion,
			},
		},
		Topology: &types.RuntimeTopology{
			PrimaryNode: "neoforge",
			Nodes: []types.RuntimeNode{
				{
					ID:               "neoforge",
					Role:             types.RuntimeRoleModLoader,
					IdentityPlatform: types.PlatformNeoforge,
					Capabilities:     []types.RuntimeCapability{types.CapabilityNeoforgeMods},
				},
			},
		},
	}
	return exec, nil
}

// neoforgeModDetector detects NeoForge mods
type neoforgeModDetector struct{}

func (d *neoforgeModDetector) Name() string {
	return "neoforge mod"
}

func (d *neoforgeModDetector) Detect(
zipReader *zip.Reader,
fileHandle *os.File,
) (packages []types.Package, err error) {
	for _, f := range zipReader.File {
		if f.Name == "META-INF/neoforge.mods.toml" {
			r, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer tools.CloseReader(r, logger.Warn)

			data, err := io.ReadAll(r)
			if err != nil {
				return nil, err
			}

			modIdentifier := &externaltype.FileModLoaderIdentifier{}
			err = toml.Unmarshal(data, modIdentifier)
			if err != nil {
				return nil, err
			}

			for _, mod := range modIdentifier.Mods {
				// Skip the neoforge mod itself
				// It will be handled by the executable detector separately
				if mod.ModID == "neoforge" {
					continue
				}

				// Version
				version := types.RawVersion(mod.Version)
				if version == "${file.jarVersion}" {
					version = getForgeModVersion(zipReader)
				}

				// Parse as internal id
				p := types.Package{
					Id: types.PackageId{
						Platform: types.PlatformNeoforge,
						Name:     syntax.ToProjectName(mod.ModID),
						Version:  version,
					},
					Local: &types.PackageInstallation{
						Path: fileHandle.Name(),
					},
					Dependencies: &types.PackageDependencies{},
					Information:  &types.ProjectInformation{},
				}

				// Parse dependencies
				//
				// This provides an authentic information (rather than a remote).
				// The file is exactly what the loader checks for.
				//
				// Unexpected mod behavior is not our concern. Later we will
				// add manual dependency/conflict management features.
				deps := modIdentifier.Dependencies[mod.ModID]
				for _, dep := range deps {
					p.Dependencies.Value = append(
						p.Dependencies.Value,
						types.Dependency{
							Id: types.PackageId{
								Platform: types.PlatformNeoforge,
								Name:     syntax.ToProjectName(dep.ModID),
							},
							Constraint: parseModLoaderMavenVersionRange(dep.VersionRange),
						},
					)
				}

				// Parse info
				p.Information = &types.ProjectInformation{
					Title:   mod.DisplayName,
					Brief:   mod.Description,
					Authors: []types.Person{{Name: mod.Authors}},
					License: modIdentifier.License,
					Urls: []types.Url{
						{
							Name: "URL",
							Type: types.UrlHome,
							Url:  mod.DisplayURL,
						},
						{
							Name: "Issue Tracker",
							Type: types.UrlIssues,
							Url:  modIdentifier.IssueTrackerURL,
						},
					},
				}

				packages = append(packages, p)
			}
		}
	}

	return packages, nil
}

func init() {
	registerExecutableDetector(&neoforgeServerDetector{})
	registerModDetector(&neoforgeModDetector{})
}
