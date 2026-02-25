package detector

import (
	"archive/zip"
	"bufio"
	"io"
	"os"
	"strings"

	"github.com/mclucy/lucy/exttype"
	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/syntax"
	"github.com/mclucy/lucy/tools"
	"github.com/mclucy/lucy/types"

	"github.com/pelletier/go-toml"
)

// forgeServerDetector detects Forge servers
type forgeServerDetector struct{}

func (d *forgeServerDetector) Name() string {
	return "forge server"
}

func (d *forgeServerDetector) Detect(
	filePath string,
	zipReader *zip.Reader,
	fileHandle *os.File,
) (*types.ExecutableInfo, error) {
	forgeVersion := types.UnknownVersion
	gameVersion := types.UnknownVersion
	for _, f := range zipReader.File {
		if f.Name == "META-INF/MANIFEST.MF" {
			r, err := f.Open()
			if err != nil {
				continue
			}
			defer tools.CloseReader(r, logger.Warn)

			s := bufio.NewScanner(r)
			for s.Scan() {
				line := s.Text()
				if line == "Implementation-Title: net.minecraftforge" {
					if !s.Scan() {
						continue
					}
					line := s.Text()
					if after, found := strings.CutPrefix(
						line,
						"Implementation-Version: ",
					); found {
						forgeVersion = types.RawVersion(after)
					}
				}
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
			}

			if forgeVersion != types.UnknownVersion && gameVersion != types.UnknownVersion {
				exec := &types.ExecutableInfo{
					Path:          filePath,
					GameVersion:   gameVersion,
					ModLoader:     types.Forge,
					LoaderVersion: forgeVersion,
					BootCommand:   nil,
				}
				return exec, nil
			}
		}
	}

	return nil, nil
}

// forgeModDetector detects new Forge mods (1.13+)
type forgeModDetector struct{}

func (d *forgeModDetector) Name() string {
	return "forge mod"
}

func (d *forgeModDetector) Detect(
	zipReader *zip.Reader,
	fileHandle *os.File,
) (packages []types.Package, err error) {
	for _, f := range zipReader.File {
		if f.Name == "META-INF/mods.toml" {
			r, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer tools.CloseReader(r, logger.Warn)

			data, err := io.ReadAll(r)
			if err != nil {
				return nil, err
			}

			modIdentifier := &exttype.FileForgeModIdentifier{}
			err = toml.Unmarshal(data, modIdentifier)
			if err != nil {
				return nil, err
			}

			for _, mod := range modIdentifier.Mods {
				// Skip the forge mod itself
				// It will be handled by the executable detector separately
				if mod.ModID == "forge" {
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
						Platform: types.Forge,
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
								Platform: types.Forge,
								Name:     syntax.ToProjectName(dep.ModID),
							},
							Constraint: parseMavenVersionRange(dep.VersionRange),
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

// TODO: Old forge is not yet supported. The detector was vibe-coded and needs
// more research.

// ForgeModDetectorOld detects old Forge mods (pre-1.13)
// type ForgeModDetectorOld struct{}

// func (d *ForgeModDetectorOld) Name() string {
// 	return "ForgeModDetectorOld"
// }

// func (d *ForgeModDetectorOld) DetectMod(
// 	zipReader *zip.Reader,
// 	fileHandle *os.File,
// ) []types.Package {
// 	for _, f := range zipReader.File {
// 		if f.Name == "META-INF/mcmod.info" {
// 			r, err := f.Open()
// 			if err != nil {
// 				return nil
// 			}
// 			defer tools.CloseReader(r, logger.Warn)

// 			data, err := io.ReadAll(r)
// 			if err != nil {
// 				return nil
// 			}

// 			modInfos := &datatype.ForgeModIdentifierOld{}
// 			err = json.Unmarshal(data, modInfos)
// 			if err != nil {
// 				return nil
// 			}

// 			var packages []types.Package
// 			for _, modInfo := range *modInfos {
// 				version := types.RawVersion(modInfo.Version)
// 				if version == "${file.jarVersion}" {
// 					version = getForgeModVersion(zipReader)
// 				}

// 				p := types.Package{
// 					Id: types.PackageId{
// 						Platform: types.Forge,
// 						Name:     syntax.ToProjectName(modInfo.ModId),
// 						Version:  version,
// 					},
// 					Local: &types.PackageInstallation{
// 						Path: fileHandle.Name(),
// 					},
// 				}
// 				packages = append(packages, p)
// 			}

// 			if len(packages) > 0 {
// 				return packages
// 			}
// 			return packages
// 		}
// 	}

// 	return nil
// }

func init() {
	registerExecutableDetector(&forgeServerDetector{})
	registerModDetector(&forgeModDetector{})
}
