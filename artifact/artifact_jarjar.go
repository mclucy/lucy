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

const jarjarMetadataPath = "META-INF/jarjar/metadata.json"

// readJarjarMetadata parses the JarJar metadata shared by Forge and NeoForge.
func readJarjarMetadata(zipRdr *zip.Reader) *fileschema.FileJarjar {
	raw, err := readZipEntryBytes(zipRdr, jarjarMetadataPath)
	if err != nil || raw == nil {
		return nil
	}

	var meta fileschema.FileJarjar
	if err := json.Unmarshal(raw, &meta); err != nil {
		return nil
	}
	return &meta
}

// jarjarNestedPaths lists the archives JarJar adds to the loader's module
// layer.
func jarjarNestedPaths(meta *fileschema.FileJarjar) []string {
	if meta == nil {
		return nil
	}
	paths := make([]string, 0, len(meta.Jars))
	for _, jar := range meta.Jars {
		paths = append(paths, jar.Path)
	}
	return paths
}

// jarjarEmbeddedModIDs reads the module identities of bundled archives.
// readDescriptor decodes the nested archive with the loader's precedence.
func jarjarEmbeddedModIDs(
	zipRdr *zip.Reader,
	meta *fileschema.FileJarjar,
	readDescriptor func(*zip.Reader) (*decodedModsToml, error),
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

		descriptor, err := readDescriptor(nested)
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

// jarjarEmbeddedDependencies lists the Maven coordinates JarJar resolves.
func jarjarEmbeddedDependencies(
	meta *fileschema.FileJarjar,
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
