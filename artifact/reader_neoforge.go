package artifact

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"strings"

	"github.com/mclucy/lucy/dependency"
	"github.com/mclucy/lucy/exttype"
	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/types"

	"github.com/pelletier/go-toml"
)

const neoforgeModsTomlPath = "META-INF/mods.toml"

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
	raw, err := readZipEntry(zipRdr, neoforgeModsTomlPath)
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, nil
	}

	var modIdentifier exttype.FileModLoaderIdentifier
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

func readNeoforgeJarjarMeta(zipRdr *zip.Reader) *exttype.FileNeoforgeJarjar {
	raw, err := readZipEntry(zipRdr, "META-INF/jarjar/metadata.json")
	if err != nil || raw == nil {
		return nil
	}

	var meta exttype.FileNeoforgeJarjar
	if err := json.Unmarshal(raw, &meta); err != nil {
		return nil
	}
	return &meta
}

func neoforgeJarjarEmbeddedModIds(
	zipRdr *zip.Reader,
	meta *exttype.FileNeoforgeJarjar,
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

		raw, err := readZipEntry(nestedZip, neoforgeModsTomlPath)
		if err != nil || raw == nil {
			continue
		}

		var inner exttype.FileModLoaderIdentifier
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

func neoforgeJarjarEmbeddedDeps(meta *exttype.FileNeoforgeJarjar) []ArtifactDep {
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
	return dependency.ParseRange(
		interval,
		dependency.InferRangeDialect(types.PlatformForge),
		types.Maven,
	)
}
