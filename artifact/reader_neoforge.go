package artifact

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"strings"

	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/internal/fileschema"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/version"

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
	resolver SlugResolver,
) ([]ArtifactInfo, error) {
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
	embeddedDeps := neoforgeJarjarEmbeddedDeps(jarjarMeta)

	infos := make([]ArtifactInfo, 0, len(modIdentifier.Mods))
	for _, mod := range modIdentifier.Mods {
		if mod.ModID == "neoforge" {
			continue
		}

		version := types.BareVersion(mod.Version)
		if version == "${file.jarVersion}" {
			version = readNeoforgeManifestVersion(zipRdr)
		}

		info := ArtifactInfo{
			Ref: types.PackageRef{
				Platform: types.PlatformNeoforge,
				Name:     input.ToProjectName(mod.ModID),
			},
			Version:  version,
			FilePath: filePath,
			Metadata: types.Metadata{
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
			},
		}

		deps := modIdentifier.Dependencies[mod.ModID]
		info.Dependencies = make([]ArtifactDep, 0, len(deps)+len(embeddedDeps))
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

			info.Dependencies = append(
				info.Dependencies, ArtifactDep{
					Ref: types.PackageRef{
						Platform: types.PlatformNeoforge,
						Name:     input.ToProjectName(dep.ModID),
					},
					Constraint: parseNeoforgeMavenVersionRange(dep.VersionRange),
					Mandatory:  dep.Type == "required" || dep.Mandatory,
					Embedded:   embeddedModIds[dep.ModID],
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
	raw, err := readZipEntry(zipRdr, neoforgeModsTomlPath)
	if err != nil || raw != nil {
		return raw, err
	}

	raw, err = readZipEntry(zipRdr, legacyNeoforgeModsTomlPath)
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

func readZipEntry(zipRdr *zip.Reader, name string) ([]byte, error) {
	for _, f := range zipRdr.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		raw, err := io.ReadAll(rc)
		closeErr := rc.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
		return raw, nil
	}
	return nil, nil
}

func readNeoforgeManifestVersion(zipRdr *zip.Reader) types.BareVersion {
	raw, err := readZipEntry(zipRdr, "META-INF/MANIFEST.MF")
	if err != nil || raw == nil {
		return types.VersionUnknown
	}

	manifest := string(raw)
	const versionField = "Implementation-Version: "
	_, version, ok := strings.Cut(manifest, versionField)
	if !ok {
		return types.VersionUnknown
	}
	version = strings.Split(version, "\r")[0]
	version = strings.Split(version, "\n")[0]
	return types.BareVersion(version)
}

func readNeoforgeJarjarMeta(zipRdr *zip.Reader) *fileschema.FileNeoforgeJarjar {
	raw, err := readZipEntry(zipRdr, "META-INF/jarjar/metadata.json")
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
		jarBytes, err := io.ReadAll(rc)
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

func neoforgeJarjarEmbeddedDeps(meta *fileschema.FileNeoforgeJarjar) []ArtifactDep {
	if meta == nil {
		return nil
	}

	deps := make([]ArtifactDep, 0, len(meta.Jars))
	for _, entry := range meta.Jars {
		deps = append(
			deps, ArtifactDep{
				Ref: types.PackageRef{
					Platform: types.PlatformNone,
					Name:     input.ToProjectName(entry.Identifier.Group + ":" + entry.Identifier.Artifact),
				},
				Constraint: parseNeoforgeMavenVersionRange(entry.Version.Range),
				Mandatory:  true,
				Embedded:   true,
			},
		)
	}
	return deps
}

func parseNeoforgeMavenVersionRange(interval string) types.VersionExpr {
	return version.ParseRange(
		interval,
		version.InferRangeDialect(types.PlatformForge),
		types.Maven,
	)
}
