package artifact

import (
	"archive/zip"
	"bytes"
	"encoding/json/v2"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/internal/fileschema"
	"github.com/mclucy/lucy/internal/fsutil"
	"github.com/mclucy/lucy/version"
)

const (
	neoforgeModsTomlPath       = "META-INF/neoforge.mods.toml"
	legacyNeoforgeModsTomlPath = "META-INF/mods.toml"
)

type neoforgeReader struct{}

var _ Reader = (*neoforgeReader)(nil)

func newNeoforgeReader() Reader {
	return &neoforgeReader{}
}

func (r *neoforgeReader) Read(
	zipRdr *zip.Reader,
	filePath string,
) ([]PackageInfo, error) {
	decoded, err := decodeNeoForgeDescriptor(zipRdr, "", true, true)
	if err != nil {
		return nil, err
	}
	return packageInfosFromDecoded(filePath, decoded)
}

// decodeNeoForgeDescriptor reads the NeoForge descriptor and its JarJar
// containers. requireMarker restricts the legacy shared mods.toml to files
// that name the NeoForge loader.
func decodeNeoForgeDescriptor(
	zipRdr *zip.Reader,
	member string,
	requireMarker bool,
	packageView bool,
) (*decodedDescriptor, error) {
	descriptor, err := readNeoforgeDescriptor(zipRdr, requireMarker)
	if err != nil {
		return nil, err
	}
	jarjarMeta := readNeoforgeJarjarMeta(zipRdr)
	if descriptor == nil {
		// A jar without its own descriptor may ship its mods only through
		// JarJar containers, which FML loads from the outer archive.
		if jarjarMeta == nil || len(jarjarMeta.Jars) == 0 {
			return nil, nil
		}
		return &decodedDescriptor{
			Nested: neoforgeJarjarPaths(jarjarMeta),
		}, nil
	}

	var origins map[string]catalog.DependencyOrigin
	var embeddedDependencies []Dependency
	if packageView {
		embeddedModIDs := neoforgeJarjarEmbeddedModIDs(zipRdr, jarjarMeta)
		origins = make(map[string]catalog.DependencyOrigin, len(embeddedModIDs))
		for id := range embeddedModIDs {
			origins[id] = catalog.DependencyEmbedded
		}
		embeddedDependencies, err = neoforgeJarjarEmbeddedDependencies(
			jarjarMeta,
		)
		if err != nil {
			return nil, err
		}
	}

	decoded, err := forgeDecodedDescriptor(
		zipRdr,
		descriptor,
		member,
		catalog.EcosystemNeoForge,
		packageView,
		origins,
		embeddedDependencies,
	)
	if err != nil {
		return nil, err
	}
	if jarjarMeta != nil {
		decoded.Nested = neoforgeJarjarPaths(jarjarMeta)
	}
	return decoded, nil
}

// neoforgeJarjarPaths lists the containers FML loads from the outer archive.
func neoforgeJarjarPaths(meta *fileschema.FileNeoforgeJarjar) []string {
	paths := make([]string, 0, len(meta.Jars))
	for _, jar := range meta.Jars {
		paths = append(paths, jar.Path)
	}
	return paths
}

func readNeoforgeDescriptor(
	zipRdr *zip.Reader,
	requireMarker bool,
) (*decodedModsToml, error) {
	descriptor, err := readModsToml(zipRdr, neoforgeModsTomlPath)
	if err != nil || descriptor != nil {
		return descriptor, err
	}

	descriptor, err = readModsToml(zipRdr, legacyNeoforgeModsTomlPath)
	if err != nil || descriptor == nil {
		return descriptor, err
	}
	if requireMarker && !isNeoforgeModIdentifier(descriptor.Identifier) {
		return nil, nil
	}
	return descriptor, nil
}

func isNeoforgeModIdentifier(
	modIdentifier fileschema.FileModLoaderIdentifier,
) bool {
	// Legacy NeoForge and Forge both use mods.toml, so loader identity comes
	// from the dependency target: NeoForge docs use modId="neoforge".
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

func readNeoforgeJarjarMeta(
	zipRdr *zip.Reader,
) *fileschema.FileNeoforgeJarjar {
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

func neoforgeJarjarEmbeddedModIDs(
	zipRdr *zip.Reader,
	meta *fileschema.FileNeoforgeJarjar,
) map[string]bool {
	if meta == nil {
		return nil
	}

	byName := make(map[string]*zip.File, len(zipRdr.File))
	for _, file := range zipRdr.File {
		byName[file.Name] = file
	}

	modIDs := make(map[string]bool)
	for _, entry := range meta.Jars {
		file, ok := byName[entry.Path]
		if !ok {
			continue
		}

		reader, err := file.Open()
		if err != nil {
			continue
		}
		jarBytes, err := fsutil.CopyBytes(reader, fsutil.MaxZipEntryBytes)
		_ = reader.Close()
		if err != nil {
			continue
		}

		nested, err := zip.NewReader(
			bytes.NewReader(jarBytes),
			int64(len(jarBytes)),
		)
		if err != nil {
			continue
		}

		descriptor, err := readNeoforgeDescriptor(nested, true)
		if err != nil || descriptor == nil {
			continue
		}
		for _, mod := range descriptor.Identifier.Mods {
			if mod.ModID != "" {
				modIDs[mod.ModID] = true
			}
		}
	}

	return modIDs
}

func neoforgeJarjarEmbeddedDependencies(
	meta *fileschema.FileNeoforgeJarjar,
) ([]Dependency, error) {
	if meta == nil {
		return nil, nil
	}

	dependencies := make([]Dependency, 0, len(meta.Jars))
	for _, entry := range meta.Jars {
		constraint, err := version.ParseConstraint(
			entry.Version.Range,
			version.DialectMaven,
		)
		if err != nil {
			return nil, err
		}
		dependencies = append(
			dependencies,
			Dependency{
				Ref: catalog.VersionedRef{
					Ref: catalog.Ref{
						Name: input.ToProjectName(
							entry.Identifier.Group + ":" +
								entry.Identifier.Artifact,
						),
						Provider: catalog.ProviderUnknown,
					},
					Ecosystem: catalog.EcosystemUnspecified,
				},
				Constraint: constraint,
				Mandatory:  true,
				Origin:     catalog.DependencyEmbedded,
			},
		)
	}
	return dependencies, nil
}
