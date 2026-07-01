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
	resolver SlugResolver,
) ([]Info, error) {
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
) ([]Info, error) {
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

	infos := make([]Info, 0, len(modIdentifier.Mods))
	for _, mod := range modIdentifier.Mods {
		if mod.ModID == "forge" {
			continue
		}

		version := types.BareVersion(mod.Version)
		if version == "${file.jarVersion}" {
			version = forgeJarVersion(zipRdr)
		}

		infos = append(
			infos, Info{
				Ref: types.PackageRef{
					Eco:  types.EcoForge,
					Name: types.BarePackageName(mod.ModID),
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
	modIdentifier fileschema.FileModLoaderIdentifier,
	modID string,
) []Dependency {
	deps := modIdentifier.Dependencies[modID]
	translated := make([]Dependency, 0, len(deps))
	for _, dep := range deps {
		translated = append(
			translated, Dependency{
				Ref: types.PackageRef{
					Eco:  types.EcoForge,
					Name: types.BarePackageName(dep.ModID),
				},
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
		version.InferRangeDialect(types.EcoForge),
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
