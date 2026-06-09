package artifact

import (
	"archive/zip"
	"io"
	"strings"

	"github.com/mclucy/lucy/dependency"
	"github.com/mclucy/lucy/exttype"
	"github.com/mclucy/lucy/types"

	"github.com/pelletier/go-toml"
)

type forgeReader struct{}

var _ = newForgeReader

func newForgeReader() Reader { return &forgeReader{} }

func (r *forgeReader) Read(
	zipRdr *zip.Reader,
	filePath string,
	resolver SlugResolver,
) ([]ArtifactInfo, error) {
	_ = r
	_ = resolver

	for _, file := range zipRdr.File {
		if file.Name != "META-INF/mods.toml" {
			continue
		}

		return readForgeModsToml(zipRdr, file, filePath)
	}

	return nil, nil
}

func readForgeModsToml(
	zipRdr *zip.Reader,
	file *zip.File,
	filePath string,
) ([]ArtifactInfo, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	var modIdentifier exttype.FileModLoaderIdentifier
	if err := toml.Unmarshal(data, &modIdentifier); err != nil {
		return nil, err
	}

	infos := make([]ArtifactInfo, 0, len(modIdentifier.Mods))
	for _, mod := range modIdentifier.Mods {
		if mod.ModID == "forge" {
			continue
		}

		version := types.BareVersion(mod.Version)
		if version == "${file.jarVersion}" {
			version = forgeJarVersion(zipRdr)
		}

		infos = append(
			infos, ArtifactInfo{
				Ref: types.PackageRef{
					Platform: types.PlatformForge,
					Name:     types.BarePackageName(mod.ModID),
				},
				Version:      version,
				FilePath:     filePath,
				Dependencies: forgeDependencies(modIdentifier, mod.ModID),
				Metadata: types.Metadata{
					Title:       mod.DisplayName,
					Brief:       mod.Description,
					Description: mod.Description,
					Authors:     []types.Person{{Name: mod.Authors}},
					License:     modIdentifier.License,
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
			},
		)
	}

	return infos, nil
}

func forgeDependencies(
	modIdentifier exttype.FileModLoaderIdentifier,
	modID string,
) []ArtifactDep {
	deps := modIdentifier.Dependencies[modID]
	translated := make([]ArtifactDep, 0, len(deps))
	for _, dep := range deps {
		translated = append(
			translated, ArtifactDep{
				Ref: types.PackageRef{
					Platform: types.PlatformForge,
					Name:     types.BarePackageName(dep.ModID),
				},
				Constraint: forgeVersionRange(dep.VersionRange),
				Mandatory:  dep.Mandatory,
			},
		)
	}
	return translated
}

func forgeVersionRange(versionRange string) types.VersionExpr {
	return dependency.ParseRange(
		versionRange,
		dependency.InferRangeDialect(types.PlatformForge),
		types.Maven,
	)
}

func forgeJarVersion(zipRdr *zip.Reader) types.BareVersion {
	for _, file := range zipRdr.File {
		if file.Name != "META-INF/MANIFEST.MF" {
			continue
		}

		reader, err := file.Open()
		if err != nil {
			return types.VersionUnknown
		}

		data, err := io.ReadAll(reader)
		if closeErr := reader.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
		if err != nil {
			return types.VersionUnknown
		}

		return forgeManifestVersion(string(data))
	}

	return types.VersionUnknown
}

func forgeManifestVersion(manifest string) types.BareVersion {
	const versionField = "Implementation-Version: "
	version, found := strings.CutPrefix(manifest, versionField)
	if !found {
		_, version, found = strings.Cut(manifest, "\n"+versionField)
	}
	if !found {
		return types.VersionUnknown
	}

	version = strings.Split(version, "\r")[0]
	version = strings.Split(version, "\n")[0]
	return types.BareVersion(version)
}
