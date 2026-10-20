package artifact

import (
	"archive/zip"
	"strings"

	"github.com/mclucy/lucy/internal/fileschema"
	"github.com/mclucy/lucy/internal/fsutil"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/version"

	"github.com/pelletier/go-toml"
)

type forgeReader struct{}

var _ = newForgeReader

func newForgeReader() Reader { return &forgeReader{} }

func (r *forgeReader) Read(
	zipRdr *zip.Reader,
	filePath string,
) ([]PackageInfo, error) {
	_ = r
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
) ([]PackageInfo, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	data, err := fsutil.CopyBytes(reader, fsutil.MaxZipEntryBytes)
	if err != nil {
		return nil, err
	}

	var modIdentifier fileschema.FileModLoaderIdentifier
	if err := toml.Unmarshal(data, &modIdentifier); err != nil {
		return nil, err
	}
	// Forge and legacy NeoForge share META-INF/mods.toml. Forge docs identify the
	// loader with modId="forge"; legacy NeoForge uses modId="neoforge" instead.
	// Docs: https://docs.minecraftforge.net/en/1.21.x/gettingstarted/modfiles/
	// Docs: https://docs.neoforged.net/docs/1.20.4/gettingstarted/modfiles/
	if isNeoforgeModIdentifier(modIdentifier) && !hasLoaderDependency(
		modIdentifier,
		"forge",
	) {
		return nil, nil
	}

	infos := make([]PackageInfo, 0, len(modIdentifier.Mods))
	for _, mod := range modIdentifier.Mods {
		if mod.ModID == "forge" {
			continue
		}

		version := types.Version(mod.Version)
		if version == "${file.jarVersion}" {
			version = forgeJarVersion(zipRdr)
		}

		infos = append(
			infos, PackageInfo{
				Ref:          types.VersionedPackageRef{PackageRef: types.PackageRef{Name: types.PackageName(mod.ModID), Provider: types.ProviderUnknown}, Ecosystem: types.EcosystemForge, Version: version},
				FilePath:     filePath,
				Dependencies: forgeDependencies(modIdentifier, mod.ModID),
				Metadata: types.Metadata{
					Title:       mod.DisplayName,
					Brief:       mod.Description,
					Description: mod.Description,
					Authors:     []types.Person{{Name: mod.Authors}},
					License:     modIdentifier.License,
					URLs: []types.URL{
						{
							Name: "URL",
							Type: types.URLHome,
							URL:  mod.DisplayURL,
						},
						{
							Name: "Issue Tracker",
							Type: types.URLIssues,
							URL:  modIdentifier.IssueTrackerURL,
						},
					},
				},
			},
		)
	}

	return infos, nil
}

func forgeDependencies(
	modIdentifier fileschema.FileModLoaderIdentifier,
	modID string,
) []Dependency {
	deps := modIdentifier.Dependencies[modID]
	translated := make([]Dependency, 0, len(deps))
	for _, dep := range deps {
		translated = append(
			translated, Dependency{
				Ref:        types.VersionedPackageRef{PackageRef: types.PackageRef{Name: types.PackageName(dep.ModID), Provider: types.ProviderUnknown}, Ecosystem: types.EcosystemForge},
				Constraint: forgeVersionRange(dep.VersionRange),
				Mandatory:  dep.Mandatory,
			},
		)
	}
	return translated
}

func forgeVersionRange(versionRange string) types.VersionExpr {
	return version.ParseRange(
		versionRange,
		version.InferRangeDialect(types.EcosystemForge),
		types.VersionSchemeMaven,
	)
}

func forgeJarVersion(zipRdr *zip.Reader) types.Version {
	for _, file := range zipRdr.File {
		if file.Name != "META-INF/MANIFEST.MF" {
			continue
		}

		reader, err := file.Open()
		if err != nil {
			return types.VersionUnknown
		}

		data, err := fsutil.CopyBytes(reader, fsutil.MaxZipEntryBytes)
		closeErr := reader.Close()
		if err != nil {
			return types.VersionUnknown
		}
		if closeErr != nil {
			return types.VersionUnknown
		}

		return forgeManifestVersion(string(data))
	}

	return types.VersionUnknown
}

func forgeManifestVersion(manifest string) types.Version {
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
	return types.Version(version)
}
