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
	decoded, err := decodeForgeDescriptor(zipRdr, "", true, true)
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

// decodeForgeDescriptor reads META-INF/mods.toml. excludeNeoForge keeps the
// Forge reader from claiming a jar of the NeoForge line, whose shared
// descriptor does not name Forge. A jar that names Forge keeps its Forge
// identity even when it also carries neoforge.mods.toml.
func decodeForgeDescriptor(
	zipRdr *zip.Reader,
	member string,
	excludeNeoForge bool,
	packageView bool,
) (*decodedDescriptor, error) {
	descriptor, err := readModsToml(zipRdr, forgeModsTomlPath)
	if err != nil || descriptor == nil {
		return nil, err
	}
	if excludeNeoForge &&
		!hasLoaderDependency(descriptor.Identifier, "forge") {
		// Without an explicit Forge dependency, the jar belongs to the
		// NeoForge line when its shared descriptor names NeoForge or when it
		// carries the NeoForge descriptor.
		if isNeoforgeModIdentifier(descriptor.Identifier) {
			return nil, nil
		}
		raw, err := readZipEntryBytes(zipRdr, neoforgeModsTomlPath)
		if err != nil {
			return nil, err
		}
		if raw != nil {
			return nil, nil
		}
	}
	return forgeDecodedDescriptor(
		zipRdr,
		descriptor,
		member,
		catalog.EcosystemForge,
		packageView,
		nil,
		nil,
	)
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
