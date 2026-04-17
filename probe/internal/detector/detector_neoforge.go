package detector

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/json"
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
) (*ExecutableEvidence, error) {
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
	if !hasConcreteVersion(neoforgeLoaderVersion) {
		return nil, nil
	}

	// Build and return result (gameVersion may be VersionUnknown if not in manifest)
	exec := &ExecutableEvidence{
		PrimaryEntrance: filePath,
		GameVersion:     gameVersion,
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
					ID:           "neoforge",
					Role:         types.RuntimeRoleModLoader,
					Capabilities: []types.RuntimeCapability{types.CapabilityNeoforgeMods},
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
	// Read jarjar metadata once; used for both embedded modId set and dep list.
	jarjarMeta := readJarjarMeta(zipReader)
	embeddedModIds := jarjarEmbeddedModIds(zipReader, jarjarMeta)

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
					if dep.Type == "incompatible" {
						continue
					}
					if strings.EqualFold(dep.Side, "CLIENT") {
						continue
					}
					switch dep.ModID {
					case "neoforge", "forge", "minecraft", "java":
						continue
					}
					p.Dependencies.Value = append(
						p.Dependencies.Value,
						types.Dependency{
							Id: types.PackageId{
								Platform: types.PlatformNeoforge,
								Name:     syntax.ToProjectName(dep.ModID),
							},
							Constraint: parseModLoaderMavenVersionRange(dep.VersionRange),
							Mandatory:  dep.Type == "required" || dep.Mandatory,
							Embedded:   embeddedModIds[dep.ModID],
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

				// Append JarInJar embedded library dependencies from
				// META-INF/jarjar/metadata.json if present.
				// Reference: https://docs.neoforged.net/toolchain/docs/dependencies/jarinjar/
				embedded := jarjarEmbeddedDeps(jarjarMeta)
				p.Dependencies.Value = append(p.Dependencies.Value, embedded...)

				packages = append(packages, p)
			}
		}
	}

	return packages, nil
}

// readJarjarMeta parses META-INF/jarjar/metadata.json from a NeoForge mod JAR.
// Returns nil if the file is absent or cannot be parsed.
//
// Reference: https://docs.neoforged.net/toolchain/docs/dependencies/jarinjar/
func readJarjarMeta(zipReader *zip.Reader) *externaltype.FileNeoforgeJarjar {
	for _, f := range zipReader.File {
		if f.Name != "META-INF/jarjar/metadata.json" {
			continue
		}

		r, err := f.Open()
		if err != nil {
			logger.Warn(err)
			return nil
		}
		defer tools.CloseReader(r, logger.Warn)

		data, err := io.ReadAll(r)
		if err != nil {
			logger.Warn(err)
			return nil
		}

		var meta externaltype.FileNeoforgeJarjar
		if err := json.Unmarshal(data, &meta); err != nil {
			logger.Warn(err)
			return nil
		}
		return &meta
	}
	return nil
}

// jarjarEmbeddedModIds returns the set of NeoForge modIds that are physically
// bundled inside the JAR via JarInJar. It does this by opening each embedded
// nested JAR listed in the jarjar metadata and reading its neoforge.mods.toml.
//
// This is how the NeoForge mod loader itself resolves which modId a JarInJar
// entry satisfies: it reads the embedded JAR's mods.toml, not the artifact name.
func jarjarEmbeddedModIds(zipReader *zip.Reader, meta *externaltype.FileNeoforgeJarjar) map[string]bool {
	if meta == nil {
		return nil
	}

	// Index the outer ZIP entries by name for O(1) lookup.
	byName := make(map[string]*zip.File, len(zipReader.File))
	for _, f := range zipReader.File {
		byName[f.Name] = f
	}

	modIds := make(map[string]bool)
	for _, entry := range meta.Jars {
		f, ok := byName[entry.Path]
		if !ok {
			continue
		}

		// Read the embedded JAR bytes into memory so we can open it as a zip.
		rc, err := f.Open()
		if err != nil {
			logger.Warn(err)
			continue
		}
		jarBytes, err := io.ReadAll(rc)
		tools.CloseReader(rc, logger.Warn)
		if err != nil {
			logger.Warn(err)
			continue
		}

		nestedZip, err := zip.NewReader(bytes.NewReader(jarBytes), int64(len(jarBytes)))
		if err != nil {
			logger.Warn(err)
			continue
		}

		for _, nf := range nestedZip.File {
			if nf.Name != "META-INF/neoforge.mods.toml" {
				continue
			}
			nr, err := nf.Open()
			if err != nil {
				logger.Warn(err)
				break
			}
			tomlData, err := io.ReadAll(nr)
			tools.CloseReader(nr, logger.Warn)
			if err != nil {
				logger.Warn(err)
				break
			}

			var inner externaltype.FileModLoaderIdentifier
			if err := toml.Unmarshal(tomlData, &inner); err != nil {
				logger.Warn(err)
				break
			}
			for _, mod := range inner.Mods {
				if mod.ModID != "" {
					modIds[mod.ModID] = true
				}
			}
			break
		}
	}
	return modIds
}

// jarjarEmbeddedDeps converts jarjar metadata into Dependency entries with
// Embedded=true, using Maven group:artifact as the synthetic package name.
func jarjarEmbeddedDeps(meta *externaltype.FileNeoforgeJarjar) []types.Dependency {
	if meta == nil {
		return nil
	}
	deps := make([]types.Dependency, 0, len(meta.Jars))
	for _, entry := range meta.Jars {
		name := syntax.ToProjectName(entry.Identifier.Group + ":" + entry.Identifier.Artifact)
		deps = append(deps, types.Dependency{
			Id: types.PackageId{
				Platform: types.PlatformNone,
				Name:     name,
			},
			Constraint: parseModLoaderMavenVersionRange(entry.Version.Range),
			Mandatory:  true,
			Embedded:   true,
		})
	}
	return deps
}

func init() {
	registerExecutableDetector(&neoforgeServerDetector{})
	registerModDetector(&neoforgeModDetector{})
}
