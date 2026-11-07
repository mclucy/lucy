package artifact

import (
	"archive/zip"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/internal/fileschema"
	"github.com/mclucy/lucy/internal/fsutil"

	"github.com/mclucy/lucy/version"
	"github.com/pelletier/go-toml/v2"
)

type forgeReader struct{}

func newForgeReader() Reader { return &forgeReader{} }

func (r *forgeReader) Read(
	zipRdr *zip.Reader,
	filePath string,
) ([]PackageInfo, error) {
	neoforgeLayout := false
	for _, file := range zipRdr.File {
		if file.Name == neoforgeModsTomlPath {
			neoforgeLayout = true
			break
		}
	}
	for _, file := range zipRdr.File {
		if file.Name != "META-INF/mods.toml" {
			continue
		}

		return readForgeModsToml(zipRdr, file, filePath, neoforgeLayout)
	}

	return nil, nil
}

func readForgeModsToml(
	zipRdr *zip.Reader,
	file *zip.File,
	filePath string,
	neoforgeLayout bool,
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
	// Modern NeoForge ships META-INF/neoforge.mods.toml beside the shared file,
	// so that file alone decides the loader unless the shared file names Forge.
	// Docs: https://docs.minecraftforge.net/en/1.21.x/gettingstarted/modfiles/
	// Docs: https://docs.neoforged.net/docs/1.20.4/gettingstarted/modfiles/
	if !hasLoaderDependency(modIdentifier, "forge") &&
		(neoforgeLayout || isNeoforgeModIdentifier(modIdentifier)) {
		return nil, nil
	}

	infos := make([]PackageInfo, 0, len(modIdentifier.Mods))
	for _, mod := range modIdentifier.Mods {
		if mod.ModID == "forge" {
			continue
		}

		version := version.Version(mod.Version)
		if version == "${file.jarVersion}" {
			version = forgeJarVersion(zipRdr)
		}

		dependencies, err := forgeDependencies(modIdentifier, mod.ModID)
		if err != nil {
			return nil, err
		}
		infos = append(
			infos, PackageInfo{
				Ref:          catalog.VersionedRef{Ref: catalog.Ref{Name: catalog.Name(mod.ModID), Provider: catalog.ProviderUnknown}, Ecosystem: catalog.EcosystemForge, Version: version},
				FilePath:     filePath,
				Dependencies: dependencies,
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
			},
		)
	}

	return infos, nil
}

func forgeDependencies(
	modIdentifier fileschema.FileModLoaderIdentifier,
	modID string,
) ([]Dependency, error) {
	deps := modIdentifier.Dependencies[modID]
	translated := make([]Dependency, 0, len(deps))
	for _, dep := range deps {
		constraint, err := forgeVersionRange(dep.VersionRange)
		if err != nil {
			return nil, err
		}
		translated = append(
			translated, Dependency{
				Ref:        catalog.VersionedRef{Ref: catalog.Ref{Name: catalog.Name(dep.ModID), Provider: catalog.ProviderUnknown}, Ecosystem: catalog.EcosystemForge},
				Constraint: constraint,
				Mandatory:  dep.Mandatory,
			},
		)
	}
	return translated, nil
}

func forgeVersionRange(versionRange string) (version.Expr, error) {
	return version.ParseConstraint(versionRange, version.DialectMaven)
}

func forgeJarVersion(zipRdr *zip.Reader) version.Version {
	v, err := readManifestImplementationVersion(
		zipRdr,
		fsutil.MaxZipEntryBytes,
	)
	if err != nil {
		return version.Unknown
	}
	return v
}
