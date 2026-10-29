package artifact

import (
	"archive/zip"
	"bytes"
	"encoding/json/v2"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/internal/fileschema"
	"github.com/mclucy/lucy/internal/fsutil"

	versionpkg "github.com/mclucy/lucy/version"
	"github.com/pelletier/go-toml"
)

const (
	neoforgeModsTomlPath       = "META-INF/neoforge.mods.toml"
	legacyNeoforgeModsTomlPath = "META-INF/mods.toml"
)

type neoforgeReader struct{}

var _ = newNeoforgeReader

func newNeoforgeReader() Reader {
	return &neoforgeReader{}
}

func (r *neoforgeReader) Read(
	zipRdr *zip.Reader,
	filePath string,
) ([]PackageInfo, error) {
	raw, err := readNeoforgeModsToml(zipRdr)
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, nil
	}

	var modIdentifier fileschema.FileModLoaderIdentifier
	if err := toml.Unmarshal(raw, &modIdentifier); err != nil {
		return nil, err
	}

	jarjarMeta := readNeoforgeJarjarMeta(zipRdr)
	embeddedModIds := neoforgeJarjarEmbeddedModIds(zipRdr, jarjarMeta)
	embeddedDeps, err := neoforgeJarjarEmbeddedDeps(jarjarMeta)
	if err != nil {
		return nil, err
	}

	infos := make([]PackageInfo, 0, len(modIdentifier.Mods))
	for _, mod := range modIdentifier.Mods {
		if mod.ModID == "neoforge" {
			continue
		}

		version := versionpkg.Version(mod.Version)
		if version == "${file.jarVersion}" {
			version = readNeoforgeManifestVersion(zipRdr)
		}

		info := PackageInfo{
			Ref:      catalog.VersionedRef{Ref: catalog.Ref{Name: input.ToProjectName(mod.ModID), Provider: catalog.ProviderUnknown}, Ecosystem: catalog.EcosystemNeoForge, Version: version},
			FilePath: filePath,
			Metadata: catalog.Metadata{
				Title:       mod.DisplayName,
				Description: mod.Description,
				Authors:     []catalog.Person{{Name: mod.Authors}},
				License:     modIdentifier.License,
				URLs: []catalog.URL{
					{
						Name: "URL",
						Type: catalog.URLHome,
						URL:  mod.DisplayURL,
					},
					{
						Name: "Issue Tracker",
						Type: catalog.URLIssues,
						URL:  modIdentifier.IssueTrackerURL,
					},
				},
			},
		}

		deps := modIdentifier.Dependencies[mod.ModID]
		info.Dependencies = make([]Dependency, 0, len(deps)+len(embeddedDeps))
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

			constraint, err := parseNeoforgeMavenVersionRange(dep.VersionRange)
			if err != nil {
				return nil, err
			}
			info.Dependencies = append(
				info.Dependencies, Dependency{
					Ref:        catalog.VersionedRef{Ref: catalog.Ref{Name: input.ToProjectName(dep.ModID), Provider: catalog.ProviderUnknown}, Ecosystem: catalog.EcosystemNeoForge},
					Constraint: constraint,
					Mandatory:  dep.Type == "required" || dep.Mandatory,
					Origin:     dependencyOriginForEmbedded(embeddedModIds[dep.ModID]),
				},
			)
		}
		info.Dependencies = append(info.Dependencies, embeddedDeps...)

		infos = append(infos, info)
	}

	return infos, nil
}

func readNeoforgeModsToml(zipRdr *zip.Reader) ([]byte, error) {
	// NeoForge 1.20.5+ uses META-INF/neoforge.mods.toml; earlier 1.20.3-1.20.4
	// used META-INF/mods.toml before the rename.
	// Docs: https://docs.neoforged.net/docs/gettingstarted/modfiles/
	// Docs: https://docs.neoforged.net/docs/1.20.4/gettingstarted/modfiles/
	// Rename note: https://neoforged.net/news/20.5release/
	raw, err := readZipEntryBytes(zipRdr, neoforgeModsTomlPath)
	if err != nil || raw != nil {
		return raw, err
	}

	raw, err = readZipEntryBytes(zipRdr, legacyNeoforgeModsTomlPath)
	if err != nil || raw == nil {
		return raw, err
	}

	var modIdentifier fileschema.FileModLoaderIdentifier
	if err := toml.Unmarshal(raw, &modIdentifier); err != nil {
		return nil, err
	}
	if !isNeoforgeModIdentifier(modIdentifier) {
		return nil, nil
	}
	return raw, nil
}

func isNeoforgeModIdentifier(modIdentifier fileschema.FileModLoaderIdentifier) bool {
	// Legacy NeoForge and Forge both use mods.toml, so loader identity comes from
	// the dependency target: NeoForge docs use modId="neoforge".
	// Docs: https://docs.neoforged.net/docs/1.20.4/gettingstarted/modfiles/
	return hasLoaderDependency(modIdentifier, "neoforge")
}

func hasLoaderDependency(
	modIdentifier fileschema.FileModLoaderIdentifier,
	modID string,
) bool {
	for _, deps := range modIdentifier.Dependencies {
		for _, dep := range deps {
			if dep.ModID == modID {
				return true
			}
		}
	}
	return false
}

func readNeoforgeManifestVersion(zipRdr *zip.Reader) versionpkg.Version {
	raw, err := readZipEntryBytes(zipRdr, "META-INF/MANIFEST.MF")
	if err != nil || raw == nil {
		return versionpkg.Unknown
	}

	manifest := string(raw)
	const versionField = "Implementation-Version: "
	_, version, ok := strings.Cut(manifest, versionField)
	if !ok {
		return versionpkg.Unknown
	}
	version = strings.Split(version, "\r")[0]
	version = strings.Split(version, "\n")[0]
	return versionpkg.Version(version)
}

func readNeoforgeJarjarMeta(zipRdr *zip.Reader) *fileschema.FileNeoforgeJarjar {
	raw, err := readZipEntryBytes(zipRdr, "META-INF/jarjar/metadata.json")
	if err != nil || raw == nil {
		return nil
	}

	var meta fileschema.FileNeoforgeJarjar
	if err := json.Unmarshal(raw, &meta); err != nil {
		return nil
	}
	return &meta
}

func neoforgeJarjarEmbeddedModIds(
	zipRdr *zip.Reader,
	meta *fileschema.FileNeoforgeJarjar,
) map[string]bool {
	if meta == nil {
		return nil
	}

	byName := make(map[string]*zip.File, len(zipRdr.File))
	for _, f := range zipRdr.File {
		byName[f.Name] = f
	}

	modIds := make(map[string]bool)
	for _, entry := range meta.Jars {
		f, ok := byName[entry.Path]
		if !ok {
			continue
		}

		rc, err := f.Open()
		if err != nil {
			continue
		}
		jarBytes, err := fsutil.CopyBytes(rc, fsutil.MaxZipEntryBytes)
		_ = rc.Close()
		if err != nil {
			continue
		}

		nestedZip, err := zip.NewReader(
			bytes.NewReader(jarBytes),
			int64(len(jarBytes)),
		)
		if err != nil {
			continue
		}

		raw, err := readNeoforgeModsToml(nestedZip)
		if err != nil || raw == nil {
			continue
		}

		var inner fileschema.FileModLoaderIdentifier
		if err := toml.Unmarshal(raw, &inner); err != nil {
			continue
		}
		for _, mod := range inner.Mods {
			if mod.ModID != "" {
				modIds[mod.ModID] = true
			}
		}
	}

	return modIds
}

func neoforgeJarjarEmbeddedDeps(meta *fileschema.FileNeoforgeJarjar) ([]Dependency, error) {
	if meta == nil {
		return nil, nil
	}

	deps := make([]Dependency, 0, len(meta.Jars))
	for _, entry := range meta.Jars {
		constraint, err := parseNeoforgeMavenVersionRange(entry.Version.Range)
		if err != nil {
			return nil, err
		}
		deps = append(
			deps, Dependency{
				Ref:        catalog.VersionedRef{Ref: catalog.Ref{Name: input.ToProjectName(entry.Identifier.Group + ":" + entry.Identifier.Artifact), Provider: catalog.ProviderUnknown}, Ecosystem: catalog.EcosystemUnspecified},
				Constraint: constraint,
				Mandatory:  true,
				Origin:     catalog.DependencyEmbedded,
			},
		)
	}
	return deps, nil
}

func parseNeoforgeMavenVersionRange(interval string) (versionpkg.Expr, error) {
	return versionpkg.ParseConstraint(interval, versionpkg.DialectMaven)
}
