package artifact

import (
	"archive/zip"
	"bytes"
	"encoding/json/v2"
	"fmt"
	"sort"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/internal/fileschema"
	"github.com/mclucy/lucy/version"
)

// quiltReader reads schema_version 1 quilt.mod.json descriptors. A Quilt mod
// may also carry fabric.mod.json for Fabric compatibility, and the Quilt
// descriptor wins when both are present.
type quiltReader struct{}

var _ Reader = (*quiltReader)(nil)

func newQuiltReader() Reader { return &quiltReader{} }

func (r *quiltReader) Read(
	zipRdr *zip.Reader,
	filePath string,
) ([]PackageInfo, error) {
	raw, err := readZipEntryBytes(zipRdr, quiltModPath)
	if err != nil || raw == nil {
		return nil, err
	}
	descriptor := &quiltModFile{}
	if err := json.Unmarshal(raw, descriptor); err != nil {
		return nil, err
	}
	if err := validateQuiltDescriptor(descriptor); err != nil {
		return nil, err
	}
	info, err := translateQuiltArtifact(zipRdr, descriptor, filePath)
	if err != nil {
		return nil, err
	}
	return []PackageInfo{info}, nil
}

func translateQuiltArtifact(
	zipRdr *zip.Reader,
	modInfo *quiltModFile,
	filePath string,
) (PackageInfo, error) {
	loader := &modInfo.Loader
	embeddedNames, err := quiltArtifactEmbeddedNames(zipRdr, loader)
	if err != nil {
		return PackageInfo{}, err
	}

	dependencies := make(
		[]Dependency,
		0,
		len(loader.Depends)+len(loader.Breaks),
	)
	for _, entry := range loader.Depends {
		alternatives := entry.serverAlternatives()
		if len(alternatives) == 0 {
			continue
		}
		// A depends array is any-of. Each alternative keeps its own optional
		// flag, matching the descriptor module representation.
		converted := make([]Dependency, 0, len(alternatives))
		for _, alternative := range alternatives {
			kind := catalog.DependencyKindRequired
			if alternative.Optional {
				kind = catalog.DependencyKindOptional
			}
			dependency, err := quiltArtifactDependency(
				alternative,
				kind,
				embeddedNames,
				false,
			)
			if err != nil {
				return PackageInfo{}, err
			}
			converted = append(converted, dependency)
		}
		primary := converted[0]
		primary.AnyOf = converted[1:]
		dependencies = append(dependencies, primary)
	}
	for _, entry := range loader.Breaks {
		alternatives := entry.serverAlternatives()
		if len(alternatives) == 0 {
			continue
		}
		converted, err := quiltArtifactAlternatives(
			alternatives,
			catalog.DependencyKindIncompatible,
			embeddedNames,
			true,
		)
		if err != nil {
			return PackageInfo{}, err
		}
		primary := converted[0]
		primary.AllOf = converted[1:]
		dependencies = append(dependencies, primary)
	}

	return PackageInfo{
		Ref: catalog.VersionedRef{
			Ref: catalog.Ref{
				Name:     input.ToProjectName(loader.ID),
				Provider: catalog.ProviderUnknown,
			},
			Ecosystem: catalog.EcosystemQuilt,
			Version:   version.Version(loader.Version),
		},
		FilePath:     filePath,
		Dependencies: dependencies,
		Metadata:     quiltArtifactMetadata(loader.Metadata),
	}, nil
}

func quiltArtifactAlternatives(
	alternatives []quiltDependencyEntry,
	kind catalog.DependencyKind,
	embeddedNames map[string]struct{},
	negate bool,
) ([]Dependency, error) {
	converted := make([]Dependency, 0, len(alternatives))
	for _, alternative := range alternatives {
		dependency, err := quiltArtifactDependency(
			alternative,
			kind,
			embeddedNames,
			negate,
		)
		if err != nil {
			return nil, err
		}
		converted = append(converted, dependency)
	}
	return converted, nil
}

func quiltArtifactDependency(
	entry quiltDependencyEntry,
	kind catalog.DependencyKind,
	embeddedNames map[string]struct{},
	negate bool,
) (Dependency, error) {
	constraint, err := parseQuiltVersions(entry.Versions)
	if err != nil {
		return Dependency{}, err
	}
	if negate {
		constraint = constraint.Negate()
	}

	name := quiltDependencyName(entry.ID)
	origin := dependencyOriginForEmbedded(
		quiltEmbeddedName(embeddedNames, entry.ID),
	)
	switch quiltDependencyServerID(name) {
	case true:
		origin = catalog.DependencyServerProvided
	}

	dependency := Dependency{
		Ref: catalog.VersionedRef{
			Ref: catalog.Ref{
				Name:     input.ToProjectName(name),
				Provider: catalog.ProviderUnknown,
			},
			Ecosystem: catalog.EcosystemQuilt,
		},
		Constraint: constraint,
		Kind:       kind,
		Mandatory:  kind == catalog.DependencyKindRequired,
		Origin:     origin,
	}
	dependency.Unless, err = quiltArtifactConditions(entry.Unless)
	if err != nil {
		return Dependency{}, err
	}
	return dependency, nil
}

func quiltArtifactConditions(
	entry *quiltDependencyEntry,
) ([]Dependency, error) {
	if entry == nil {
		return nil, nil
	}
	var conditions []Dependency
	for _, alternative := range entry.serverAlternatives() {
		constraint, err := parseQuiltVersions(alternative.Versions)
		if err != nil {
			return nil, err
		}
		condition := Dependency{
			Ref: catalog.VersionedRef{
				Ref: catalog.Ref{
					Name:     input.ToProjectName(quiltDependencyName(alternative.ID)),
					Provider: catalog.ProviderUnknown,
				},
				Ecosystem: catalog.EcosystemQuilt,
			},
			Constraint: constraint,
			Kind:       catalog.DependencyKindRequired,
		}
		condition.Unless, err = quiltArtifactConditions(alternative.Unless)
		if err != nil {
			return nil, err
		}
		conditions = append(conditions, condition)
	}
	return conditions, nil
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

// quiltArtifactEmbeddedNames lists the identities the jars field bundles.
// Nested jars carry their own descriptor, and a jar without one falls back to
// the filename convention used by Fabric wrappers.
func quiltArtifactEmbeddedNames(
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
	raw, err := readZipEntryBytes(nested, quiltModPath)
	if err != nil {
		return false, err
	}
	if raw != nil {
		descriptor := &quiltModFile{}
		if err := json.Unmarshal(raw, descriptor); err != nil {
			return false, err
		}
		if err := validateQuiltDescriptor(descriptor); err != nil {
			return false, err
		}
		quiltRecordNames(names, descriptor.Loader.ID)
		for _, provided := range descriptor.Loader.Provides {
			quiltRecordNames(names, provided.ID)
		}
		return true, nil
	}

	raw, err = readZipEntryBytes(nested, "fabric.mod.json")
	if err != nil {
		return false, err
	}
	if raw == nil {
		return false, nil
	}
	fabric := &fileschema.FileFabricModIdentifier{}
	if err := json.Unmarshal(raw, fabric); err != nil {
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

// quiltArchiveModule converts one quilt_loader descriptor into the module the
// resolver indexes. It returns nil for a client-only descriptor.
func quiltArchiveModule(
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
		VersionDialect:   version.DialectFabric,
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

// quiltDescriptorDependencies translates depends and breaks. Entries are
// independent requirements; a nested array in depends offers any-of
// alternatives, and a nested array in breaks applies every entry.
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
		primary.AnyOf = converted[1:]
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
		primary.AllOf = converted[1:]
		dependencies = append(dependencies, primary)
	}
	return dependencies, nil
}

func quiltDescriptorDependency(
	entry quiltDependencyEntry,
	kind catalog.DependencyKind,
) (catalog.DescriptorDependency, error) {
	expr, err := parseQuiltVersions(entry.Versions)
	if err != nil {
		return catalog.DescriptorDependency{}, err
	}
	constraint, err := renderVersionExpr(expr)
	if err != nil {
		return catalog.DescriptorDependency{}, err
	}
	dependency := catalog.DescriptorDependency{
		ID:         entry.ID,
		Kind:       kind,
		Constraint: constraint,
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
		expr, err := parseQuiltVersions(alternative.Versions)
		if err != nil {
			return nil, err
		}
		constraint, err := renderVersionExpr(expr)
		if err != nil {
			return nil, err
		}
		condition := catalog.DescriptorDependency{
			ID:         alternative.ID,
			Kind:       catalog.DependencyKindRequired,
			Constraint: constraint,
		}
		condition.Unless, err = quiltDescriptorConditions(alternative.Unless)
		if err != nil {
			return nil, err
		}
		conditions = append(conditions, condition)
	}
	return conditions, nil
}
