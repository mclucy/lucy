package artifact

import (
	"archive/zip"
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/internal/fileschema"
	"github.com/mclucy/lucy/version"
)

// quiltReader reads schema_version 1 quilt.mod.json descriptors.
type quiltReader struct{}

var _ Reader = (*quiltReader)(nil)

func newQuiltReader() Reader { return &quiltReader{} }

func (r *quiltReader) Read(
	zipRdr *zip.Reader,
	filePath string,
) ([]PackageInfo, error) {
	decoded, err := decodeQuiltDescriptor(zipRdr, "", true)
	if err != nil {
		return nil, err
	}
	return packageInfosFromDecoded(filePath, decoded)
}

// decodeQuiltDescriptor reads quilt.mod.json and returns nil when the archive
// does not carry it. packageView adds the metadata ReadInfo reports.
func decodeQuiltDescriptor(
	zipRdr *zip.Reader,
	member string,
	packageView bool,
) (*decodedDescriptor, error) {
	descriptor, err := readQuiltDescriptor(zipRdr)
	if err != nil || descriptor == nil {
		return nil, err
	}
	loader := &descriptor.Loader
	module, err := quiltModule(descriptor, member)
	if err != nil {
		return nil, err
	}
	decoded := decodedModule{Module: *module}
	if packageView {
		embeddedNames, err := quiltEmbeddedNames(zipRdr, loader)
		if err != nil {
			return nil, err
		}
		decoded.Metadata = quiltArtifactMetadata(loader.Metadata)
		decoded.Origins = quiltDependencyOrigins(
			module.Dependencies,
			embeddedNames,
		)
		decoded.PackageNames = quiltDependencyPackageNames(
			module.Dependencies,
		)
		decoded.PackageEcosystem = catalog.EcosystemQuilt
	}
	return &decodedDescriptor{
		Modules:    []decodedModule{decoded},
		Nested:     loader.Jars,
		SkipNested: module.Side == catalog.SideClient,
	}, nil
}

func readQuiltDescriptor(zipRdr *zip.Reader) (*quiltModFile, error) {
	raw, err := readZipEntryBytes(zipRdr, quiltModPath)
	if err != nil || raw == nil {
		return nil, err
	}
	descriptor := &quiltModFile{}
	if err := unmarshalDescriptor(raw, descriptor); err != nil {
		return nil, err
	}
	if err := validateQuiltDescriptor(descriptor); err != nil {
		return nil, err
	}
	return descriptor, nil
}

// quiltModule converts one quilt_loader descriptor into the module the
// resolver indexes.
func quiltModule(
	descriptor *quiltModFile,
	member string,
) (*catalog.DescriptorModule, error) {
	loader := &descriptor.Loader
	side := catalog.ParseSide(quiltEnvironment(descriptor.Minecraft.Environment))

	provides := make([]string, 0, len(loader.Provides))
	var providedVersions map[string]version.Version
	for _, provided := range loader.Provides {
		if provided.ID == "" {
			continue
		}
		provides = append(provides, provided.ID)
		if provided.Version != "" {
			if providedVersions == nil {
				providedVersions = map[string]version.Version{}
			}
			providedVersions[provided.ID] = provided.Version
		}
	}

	dependencies, err := quiltDescriptorDependencies(loader)
	if err != nil {
		return nil, err
	}

	return &catalog.DescriptorModule{
		ID:               loader.ID,
		Version:          version.Version(loader.Version),
		Ecosystem:        catalog.EcosystemQuilt,
		VersionDialect:   version.DialectQuilt,
		NestedPath:       member,
		DescriptorPath:   quiltModPath,
		Group:            loader.Group,
		Provides:         provides,
		ProvidedVersions: providedVersions,
		Side:             side,
		Dependencies:     dependencies,
	}, nil
}

func quiltEnvironment(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "dedicated_server", "server":
		return "server"
	default:
		return raw
	}
}

// quiltDescriptorDependencies translates depends and breaks. A nested array in
// depends offers any-of alternatives, and a nested array in breaks applies
// every entry.
func quiltDescriptorDependencies(
	loader *quiltLoader,
) ([]catalog.DescriptorDependency, error) {
	dependencies := make(
		[]catalog.DescriptorDependency,
		0,
		len(loader.Depends)+len(loader.Breaks),
	)
	for _, entry := range loader.Depends {
		alternatives := entry.serverAlternatives()
		if len(alternatives) == 0 {
			continue
		}
		// A depends array is any-of. Each alternative keeps its own optional
		// flag: an absent optional alternative satisfies the group, while an
		// absent required alternative does not.
		converted := make([]catalog.DescriptorDependency, 0, len(alternatives))
		for _, alternative := range alternatives {
			kind := catalog.DependencyKindRequired
			if alternative.Optional {
				kind = catalog.DependencyKindOptional
			}
			dependency, err := quiltDescriptorDependency(alternative, kind)
			if err != nil {
				return nil, err
			}
			converted = append(converted, dependency)
		}
		primary := converted[0]
		primary.AnyOf = append(primary.AnyOf, converted[1:]...)
		dependencies = append(dependencies, primary)
	}
	for _, entry := range loader.Breaks {
		alternatives := entry.serverAlternatives()
		if len(alternatives) == 0 {
			continue
		}
		converted := make([]catalog.DescriptorDependency, 0, len(alternatives))
		for _, alternative := range alternatives {
			dependency, err := quiltDescriptorDependency(
				alternative,
				catalog.DependencyKindIncompatible,
			)
			if err != nil {
				return nil, err
			}
			converted = append(converted, dependency)
		}
		primary := converted[0]
		primary.AllOf = append(primary.AllOf, converted[1:]...)
		dependencies = append(dependencies, primary)
	}
	return dependencies, nil
}

func quiltDescriptorDependency(
	entry quiltDependencyEntry,
	kind catalog.DependencyKind,
) (catalog.DescriptorDependency, error) {
	dependency, err := quiltVersionNode(entry.Versions, entry.ID, kind)
	if err != nil {
		return catalog.DescriptorDependency{}, err
	}
	dependency.Unless, err = quiltDescriptorConditions(entry.Unless)
	if err != nil {
		return catalog.DescriptorDependency{}, err
	}
	return dependency, nil
}

func quiltDescriptorConditions(
	entry *quiltDependencyEntry,
) ([]catalog.DescriptorDependency, error) {
	if entry == nil {
		return nil, nil
	}
	var conditions []catalog.DescriptorDependency
	for _, alternative := range entry.serverAlternatives() {
		condition, err := quiltVersionNode(
			alternative.Versions,
			alternative.ID,
			catalog.DependencyKindRequired,
		)
		if err != nil {
			return nil, err
		}
		condition.Unless, err = quiltDescriptorConditions(alternative.Unless)
		if err != nil {
			return nil, err
		}
		conditions = append(conditions, condition)
	}
	return conditions, nil
}

func quiltDependencyOrigins(
	dependencies []catalog.DescriptorDependency,
	embeddedNames map[string]struct{},
) map[string]catalog.DependencyOrigin {
	origins := make(map[string]catalog.DependencyOrigin, len(dependencies))
	for _, dependency := range dependencies {
		if quiltDependencyServerID(quiltDependencyName(dependency.ID)) {
			origins[dependency.ID] = catalog.DependencyServerProvided
			continue
		}
		if quiltEmbeddedName(embeddedNames, dependency.ID) {
			origins[dependency.ID] = catalog.DependencyEmbedded
		}
	}
	return origins
}

func quiltDependencyPackageNames(
	dependencies []catalog.DescriptorDependency,
) map[string]string {
	names := make(map[string]string, len(dependencies))
	for _, dependency := range dependencies {
		names[dependency.ID] = quiltDependencyName(dependency.ID)
	}
	return names
}

// quiltDependencyName returns the project identity a dependency names. Quilt
// allows a maven group prefix, but upstream projects and package identities
// are keyed by the mod id alone.
func quiltDependencyName(id string) string {
	if _, name, ok := strings.Cut(id, ":"); ok && name != "" {
		return name
	}
	return id
}

func quiltDependencyServerID(name string) bool {
	switch name {
	case "minecraft", "java", "quilt_loader", "fabricloader":
		return true
	default:
		return false
	}
}

// quiltEmbeddedName reports whether a nested jar declares the dependency
// identity. The raw id and its group-free project name are both recorded.
func quiltEmbeddedName(names map[string]struct{}, id string) bool {
	if _, ok := names[id]; ok {
		return true
	}
	_, ok := names[quiltDependencyName(id)]
	return ok
}

func quiltArtifactMetadata(metadata *quiltMetadata) catalog.Metadata {
	if metadata == nil {
		return catalog.Metadata{}
	}
	return catalog.Metadata{
		Title:       metadata.Name,
		Description: metadata.Description,
		License:     string(metadata.License),
		Authors:     quiltArtifactAuthors(metadata.Contributors),
		URLs:        quiltArtifactURLs(metadata.Contact),
	}
}

func quiltArtifactAuthors(contributors map[string]any) []catalog.Person {
	names := make([]string, 0, len(contributors))
	for name := range contributors {
		names = append(names, name)
	}
	sort.Strings(names)

	authors := make([]catalog.Person, 0, len(names))
	for _, name := range names {
		authors = append(authors, catalog.Person{
			Name: name,
			Role: quiltRole(contributors[name]),
		})
	}
	return authors
}

func quiltArtifactURLs(contact map[string]string) []catalog.URL {
	urlSpecs := []struct {
		key     string
		name    string
		urlType catalog.URLType
	}{
		{key: "homepage", name: "Homepage", urlType: catalog.URLHome},
		{key: "sources", name: "Source", urlType: catalog.URLSource},
		{key: "issues", name: "Issues", urlType: catalog.URLIssues},
	}

	urls := make([]catalog.URL, 0, len(urlSpecs))
	for _, spec := range urlSpecs {
		url := contact[spec.key]
		if url == "" {
			continue
		}
		urls = append(
			urls,
			catalog.URL{Name: spec.name, Type: spec.urlType, URL: url},
		)
	}
	return urls
}

// quiltEmbeddedNames lists the identities the jars field bundles. Nested jars
// carry their own descriptor, and a jar without one falls back to the filename
// convention used by Fabric wrappers.
func quiltEmbeddedNames(
	zipRdr *zip.Reader,
	loader *quiltLoader,
) (map[string]struct{}, error) {
	dependencyNames := make([]string, 0, len(loader.Depends))
	for _, entry := range loader.Depends {
		for _, alternative := range entry.serverAlternatives() {
			dependencyNames = append(
				dependencyNames,
				quiltDependencyName(alternative.ID),
			)
		}
	}

	names := make(map[string]struct{}, len(loader.Jars))
	for _, jar := range loader.Jars {
		raw, err := readZipEntryBytes(zipRdr, jar)
		if err != nil {
			return nil, err
		}
		if raw == nil {
			return nil, fmt.Errorf("missing nested Quilt JAR %s", jar)
		}
		nested, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
		if err != nil {
			return nil, err
		}
		recorded, err := quiltRecordNestedNames(nested, names)
		if err != nil {
			return nil, err
		}
		if recorded {
			continue
		}

		base := jar
		if _, tail, ok := strings.CutLast(base, "/"); ok {
			base = tail
		}
		base = strings.TrimSuffix(base, ".jar")
		for _, name := range dependencyNames {
			if base == name || strings.HasPrefix(base, name+"-") {
				names[name] = struct{}{}
				break
			}
		}
	}
	return names, nil
}

func quiltRecordNestedNames(
	nested *zip.Reader,
	names map[string]struct{},
) (bool, error) {
	descriptor, err := readQuiltDescriptor(nested)
	if err != nil {
		return false, err
	}
	if descriptor != nil {
		quiltRecordNames(names, descriptor.Loader.ID)
		for _, provided := range descriptor.Loader.Provides {
			quiltRecordNames(names, provided.ID)
		}
		return true, nil
	}

	raw, err := readZipEntryBytes(nested, "fabric.mod.json")
	if err != nil {
		return false, err
	}
	if raw == nil {
		return false, nil
	}
	fabric := &fileschema.FileFabricModIdentifier{}
	if err := unmarshalDescriptor(raw, fabric); err != nil {
		return false, err
	}
	quiltRecordNames(names, fabric.ID)
	for _, provided := range fabric.Provides {
		quiltRecordNames(names, provided)
	}
	return true, nil
}

func quiltRecordNames(names map[string]struct{}, id string) {
	if id == "" {
		return
	}
	names[id] = struct{}{}
	names[quiltDependencyName(id)] = struct{}{}
}
