package artifact

import (
	"archive/zip"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/internal/fileschema"
	"github.com/mclucy/lucy/internal/fsutil"
	"github.com/mclucy/lucy/version"
	"github.com/pelletier/go-toml/v2"
)

const forgeModsTomlPath = "META-INF/mods.toml"

type forgeReader struct{}

var _ Reader = (*forgeReader)(nil)

func newForgeReader() Reader { return &forgeReader{} }

func (r *forgeReader) Read(
	zipRdr *zip.Reader,
	filePath string,
) ([]PackageInfo, error) {
	decoded, err := decodeForgeDescriptor(zipRdr, "", true)
	if err != nil {
		return nil, err
	}
	return packageInfosFromDecoded(filePath, decoded)
}

// decodedModsToml is the parsed shared Forge and NeoForge descriptor.
type decodedModsToml struct {
	Identifier fileschema.FileModLoaderIdentifier
	Path       string
}

// readModsToml parses one mods.toml entry. It returns nil when the entry is
// absent.
func readModsToml(
	zipRdr *zip.Reader,
	name string,
) (*decodedModsToml, error) {
	raw, err := readZipEntryBytes(zipRdr, name)
	if err != nil || raw == nil {
		return nil, err
	}
	var identifier fileschema.FileModLoaderIdentifier
	if err := toml.Unmarshal(raw, &identifier); err != nil {
		return nil, err
	}
	return &decodedModsToml{Identifier: identifier, Path: name}, nil
}

// decodeForgeDescriptor reads META-INF/mods.toml and every archive Forge adds
// from it. Forge ignores neoforge.mods.toml, so a jar carrying both descriptors
// stays a Forge mod. A library or language-provider jar may have no descriptor;
// its JarJar children still carry modules. A jar with an
// Embedded-Dependencies-Mod attribute contributes that archive, as Connector's
// locator does.
func decodeForgeDescriptor(
	zipRdr *zip.Reader,
	member string,
	packageView bool,
) (*decodedDescriptor, error) {
	descriptor, err := readModsToml(zipRdr, forgeModsTomlPath)
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
	service := forgeServiceProvider(zipRdr)
	if service {
		// Connector's locator names its mod archive here; FML itself reads
		// only JarJar metadata.
		if embedded := attributes["Embedded-Dependencies-Mod"]; embedded != "" {
			nested = append(nested, embedded)
		}
	}
	if descriptor == nil {
		if !forgeLoadsWithoutDescriptor(attributes["FMLModType"]) && !service {
			return nil, nil
		}
		if !service && len(nested) == 0 {
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
				return readModsToml(nested, forgeModsTomlPath)
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
		catalog.EcosystemForge,
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

// forgeLoadsWithoutDescriptor reports whether FML keeps a jar that has no
// mods.toml. Every mod file type except MOD is accepted without one.
func forgeLoadsWithoutDescriptor(modType string) bool {
	switch modType {
	case "LIBRARY", "GAMELIBRARY", "LANGPROVIDER":
		return true
	default:
		return false
	}
}

// forgeServiceProvider reports whether the jar supplies a ModLauncher or FML
// service. FML loads such a jar into its service layer and excludes it from
// the mods-folder scan, so the jar itself is not a mod. Connector uses this
// path and points at its mod archive through Embedded-Dependencies-Mod.
func forgeServiceProvider(zipRdr *zip.Reader) bool {
	for _, file := range zipRdr.File {
		switch file.Name {
		case "META-INF/services/cpw.mods.modlauncher.api.ITransformationService",
			"META-INF/services/net.minecraftforge.forgespi.locating.IModLocator",
			"META-INF/services/net.minecraftforge.forgespi.locating.IDependencyLocator",
			"META-INF/services/net.minecraftforge.fml.loading.ImmediateWindowProvider":
			return true
		}
	}
	return false
}

// forgeDecodedDescriptor converts one mods.toml descriptor into modules and
// their reader views. extras lists reader-only dependencies the descriptor
// does not declare, such as JarJar containers.
func forgeDecodedDescriptor(
	zipRdr *zip.Reader,
	descriptor *decodedModsToml,
	member string,
	ecosystem catalog.Ecosystem,
	packageView bool,
	packageOrigins map[string]catalog.DependencyOrigin,
	extras []Dependency,
) (*decodedDescriptor, error) {
	modules := make([]decodedModule, 0, len(descriptor.Identifier.Mods))
	for _, mod := range descriptor.Identifier.Mods {
		value := version.Version(mod.Version)
		if value == "${file.jarVersion}" {
			manifestVersion, err := readManifestImplementationVersion(
				zipRdr,
				fsutil.MaxNestedZipEntryBytes,
			)
			if err != nil {
				return nil, err
			}
			value = manifestVersion
		}

		module := catalog.DescriptorModule{
			ID:             mod.ModID,
			Version:        value,
			Ecosystem:      ecosystem,
			VersionDialect: version.DialectMaven,
			NestedPath:     member,
			DescriptorPath: descriptor.Path,
			Dependencies:   []catalog.DescriptorDependency{},
		}
		var metadata catalog.Metadata
		if packageView {
			metadata = catalog.Metadata{
				Title:       mod.DisplayName,
				Description: mod.Description,
				Authors:     []catalog.Person{{Name: mod.Authors}},
				License:     descriptor.Identifier.License,
				URLs: []catalog.URL{
					{
						Name: "URL",
						Type: catalog.URLHome,
						URL:  mod.DisplayURL,
					},
					{
						Name: "Issue Tracker",
						Type: catalog.URLIssues,
						URL:  descriptor.Identifier.IssueTrackerURL,
					},
				},
			}
		}
		dependencies, packageDependencies, err := forgeDependencies(
			descriptor.Identifier,
			mod.ModID,
			ecosystem,
			packageView,
			packageOrigins,
		)
		if err != nil {
			return nil, err
		}
		module.Dependencies = dependencies
		decoded := decodedModule{
			Module:      module,
			SkipPackage: forgeLoaderModID(ecosystem) == mod.ModID,
		}
		if packageView {
			decoded.Metadata = metadata
			decoded.PackageDependencies = append(
				packageDependencies,
				extras...,
			)
		}
		modules = append(modules, decoded)
	}
	return &decodedDescriptor{Modules: modules}, nil
}

func forgeLoaderModID(ecosystem catalog.Ecosystem) string {
	if ecosystem == catalog.EcosystemNeoForge {
		return "neoforge"
	}
	return "forge"
}

// forgeDependencies translates one mod's dependency list into the resolver
// view and, when requested, the reader view. Client-only entries do not
// constrain a dedicated server.
func forgeDependencies(
	identifier fileschema.FileModLoaderIdentifier,
	modID string,
	ecosystem catalog.Ecosystem,
	packageView bool,
	origins map[string]catalog.DependencyOrigin,
) ([]catalog.DescriptorDependency, []Dependency, error) {
	deps := identifier.Dependencies[modID]
	moduleDependencies := make([]catalog.DescriptorDependency, 0, len(deps))
	packageDependencies := make([]Dependency, 0, len(deps))
	for _, dep := range deps {
		if catalog.ParseSide(dep.Side) != catalog.SideClient {
			kind := catalog.ParseDependencyKind(dep.Type)
			if kind == catalog.DependencyKindUnknown {
				kind = catalog.DependencyKindOptional
				if dep.Mandatory {
					kind = catalog.DependencyKindRequired
				}
			}
			moduleDependencies = append(
				moduleDependencies,
				catalog.DescriptorDependency{
					ID:         dep.ModID,
					Constraint: dep.VersionRange,
					Kind:       kind,
					Ordering:   dep.Ordering,
				},
			)
		}

		if !packageView {
			continue
		}
		if ecosystem == catalog.EcosystemNeoForge {
			if dep.Type == "incompatible" ||
				catalog.ParseSide(dep.Side) == catalog.SideClient ||
				isLoaderDependencyID(dep.ModID) {
				continue
			}
		}
		constraint, err := version.ParseConstraint(
			dep.VersionRange,
			version.DialectMaven,
		)
		if err != nil {
			return nil, nil, err
		}
		mandatory := dep.Mandatory
		if ecosystem == catalog.EcosystemNeoForge {
			mandatory = dep.Type == "required" || dep.Mandatory
		}
		origin := catalog.DependencyRegular
		if mapped, ok := origins[dep.ModID]; ok {
			origin = mapped
		}
		packageDependencies = append(packageDependencies, Dependency{
			Ref: catalog.VersionedRef{
				Ref: catalog.Ref{
					Name:     input.ToProjectName(dep.ModID),
					Provider: catalog.ProviderUnknown,
				},
				Ecosystem: ecosystem,
			},
			Constraint: constraint,
			Mandatory:  mandatory,
			Origin:     origin,
		})
	}
	return moduleDependencies, packageDependencies, nil
}

func isLoaderDependencyID(id string) bool {
	switch id {
	case "neoforge", "forge", "minecraft", "java":
		return true
	default:
		return false
	}
}
