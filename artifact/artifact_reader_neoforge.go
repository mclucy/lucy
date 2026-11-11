package artifact

import (
	"archive/zip"
	"strconv"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/internal/fsutil"
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
	decoded, err := decodeNeoForgeDescriptor(zipRdr, "", true, "")
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
	packageView bool,
	minecraft string,
) (*decodedDescriptor, error) {
	descriptor, err := readNeoforgeDescriptor(zipRdr, minecraft)
	if err != nil {
		return nil, err
	}
	jarjarMeta := readJarjarMetadata(zipRdr)
	nested := jarjarNestedPaths(jarjarMeta)
	attributes, err := readManifestAttributes(
		zipRdr,
		fsutil.MaxNestedZipEntryBytes,
	)
	if err != nil {
		return nil, err
	}
	if descriptor == nil {
		if !forgeLoadsWithoutDescriptor(attributes["FMLModType"]) {
			return nil, nil
		}
		return &decodedDescriptor{Nested: nested}, nil
	}

	var origins map[string]catalog.DependencyOrigin
	var embeddedDependencies []Dependency
	if packageView {
		embeddedModIDs := jarjarEmbeddedModIDs(
			zipRdr,
			jarjarMeta,
			func(nested *zip.Reader) (*decodedModsToml, error) {
				return readNeoforgeDescriptor(nested, minecraft)
			},
		)
		origins = make(map[string]catalog.DependencyOrigin, len(embeddedModIDs))
		for id := range embeddedModIDs {
			origins[id] = catalog.DependencyEmbedded
		}
		embeddedDependencies, err = jarjarEmbeddedDependencies(jarjarMeta)
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
	decoded.Nested = nested
	return decoded, nil
}

// readNeoforgeDescriptor selects the descriptor path of this FML line. FML 3
// (Minecraft 1.20.6) probes neoforge.mods.toml; earlier lines probe the shared
// mods.toml. Without a server version, report whichever descriptor the jar
// carries.
func readNeoforgeDescriptor(
	zipRdr *zip.Reader,
	minecraft string,
) (*decodedModsToml, error) {
	if minecraft == "" {
		descriptor, err := readModsToml(zipRdr, neoforgeModsTomlPath)
		if err != nil || descriptor != nil {
			return descriptor, err
		}
		return readModsToml(zipRdr, legacyNeoforgeModsTomlPath)
	}
	if neoforgeReadsNeoDescriptor(minecraft) {
		return readModsToml(zipRdr, neoforgeModsTomlPath)
	}
	return readModsToml(zipRdr, legacyNeoforgeModsTomlPath)
}

// neoforgeReadsNeoDescriptor reports whether the FML line for minecraft
// probes META-INF/neoforge.mods.toml. FML 3 starts with Minecraft 1.20.6.
func neoforgeReadsNeoDescriptor(minecraft string) bool {
	major, minor, patch, ok := minecraftVersion(minecraft)
	if !ok {
		return true
	}
	if major != 1 {
		return major > 1
	}
	if minor != 20 {
		return minor > 20
	}
	return patch >= 6
}

func minecraftVersion(raw string) (major, minor, patch int, ok bool) {
	parts := strings.Split(raw, ".")
	if len(parts) < 2 {
		return 0, 0, 0, false
	}
	numbers := make([]int, 3)
	for i := 0; i < len(numbers) && i < len(parts); i++ {
		value, err := strconv.Atoi(parts[i])
		if err != nil {
			return 0, 0, 0, false
		}
		numbers[i] = value
	}
	return numbers[0], numbers[1], numbers[2], true
}
