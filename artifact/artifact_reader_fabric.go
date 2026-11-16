package artifact

import (
	"archive/zip"
	"bytes"
	"encoding/json/v2"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/internal/fileschema"
	"github.com/mclucy/lucy/internal/fn"
	"github.com/mclucy/lucy/version"
)

type fabricReader struct{}

var _ Reader = (*fabricReader)(nil)

func newFabricReader() Reader { return &fabricReader{} }

func (r *fabricReader) Read(
	zipRdr *zip.Reader,
	filePath string,
) ([]PackageInfo, error) {
	decoded, err := decodeFabricDescriptor(zipRdr, "", true)
	if err != nil {
		return nil, err
	}
	return packageInfosFromDecoded(filePath, decoded)
}

// decodeFabricDescriptor reads fabric.mod.json and returns nil when the
// archive does not carry it. packageView adds the metadata ReadInfo reports.
func decodeFabricDescriptor(
	zipRdr *zip.Reader,
	member string,
	packageView bool,
) (*decodedDescriptor, error) {
	modInfo, err := readFabricDescriptor(zipRdr)
	if err != nil || modInfo == nil {
		return nil, err
	}

	module := catalog.DescriptorModule{
		ID:             modInfo.ID,
		Version:        version.Version(modInfo.Version),
		Ecosystem:      catalog.EcosystemFabric,
		VersionDialect: version.DialectFabric,
		NestedPath:     member,
		DescriptorPath: "fabric.mod.json",
		Provides:       modInfo.Provides,
		Side:           catalog.ParseSide(string(modInfo.Environment)),
	}
	var origins map[string]catalog.DependencyOrigin
	var embeddedNames map[string]struct{}
	if packageView {
		embeddedNames, err = fabricEmbeddedModNames(zipRdr, modInfo)
		if err != nil {
			return nil, err
		}
		origins = make(map[string]catalog.DependencyOrigin)
	}
	for _, group := range []struct {
		dependencies map[string]fn.SingleOrSlice[string]
		kind         catalog.DependencyKind
	}{
		{modInfo.Depends, catalog.DependencyKindRequired},
		{modInfo.Recommends, catalog.DependencyKindRecommended},
		{modInfo.Suggests, catalog.DependencyKindSuggested},
		{modInfo.Breaks, catalog.DependencyKindIncompatible},
		{modInfo.Conflicts, catalog.DependencyKindConflict},
	} {
		for id, ranges := range group.dependencies {
			module.Dependencies = append(
				module.Dependencies,
				catalog.DescriptorDependency{
					ID:         id,
					Kind:       group.kind,
					Constraint: strings.Join(ranges, " || "),
				},
			)
			if packageView {
				origins[id] = fabricDependencyOrigin(id, embeddedNames)
			}
		}
	}

	nested := make([]string, 0, len(modInfo.Jars))
	for _, jar := range modInfo.Jars {
		nested = append(nested, jar.File)
	}
	decoded := decodedModule{
		Module:  module,
		Origins: origins,
	}
	if packageView {
		decoded.Metadata = fabricArtifactMetadata(modInfo)
		decoded.PackageEcosystem = catalog.EcosystemFabric
	}
	return &decodedDescriptor{
		Modules:    []decodedModule{decoded},
		Nested:     nested,
		SkipNested: module.Side == catalog.SideClient,
	}, nil
}

func readFabricDescriptor(
	zipRdr *zip.Reader,
) (*fileschema.FileFabricModIdentifier, error) {
	raw, err := readZipEntryBytes(zipRdr, "fabric.mod.json")
	if err != nil || raw == nil {
		return nil, err
	}
	modInfo := &fileschema.FileFabricModIdentifier{}
	if err := json.Unmarshal(raw, modInfo); err != nil {
		return nil, err
	}
	return modInfo, nil
}

func fabricDependencyOrigin(
	id string,
	embeddedNames map[string]struct{},
) catalog.DependencyOrigin {
	switch id {
	case "minecraft", "java", "fabricloader":
		return catalog.DependencyServerProvided
	}
	if _, embedded := embeddedNames[string(input.ToProjectName(id))]; embedded {
		return catalog.DependencyEmbedded
	}
	return catalog.DependencyRegular
}

func fabricArtifactMetadata(
	modInfo *fileschema.FileFabricModIdentifier,
) catalog.Metadata {
	return catalog.Metadata{
		Title:       modInfo.Name,
		Description: modInfo.Description,
		License:     strings.Join(modInfo.License, ", "),
		Authors:     fabricArtifactAuthors(modInfo.Authors),
		URLs:        fabricArtifactURLs(modInfo.Contact),
	}
}

func fabricEmbeddedModNames(
	zipRdr *zip.Reader,
	modInfo *fileschema.FileFabricModIdentifier,
) (map[string]struct{}, error) {
	depNames := make([]string, 0, len(modInfo.Depends))
	for id := range modInfo.Depends {
		depNames = append(depNames, id)
	}

	names := make(map[string]struct{}, len(modInfo.Jars))
	for _, jar := range modInfo.Jars {
		nestedInfo, err := fabricNestedModInfo(zipRdr, jar.File)
		if err != nil {
			return nil, err
		}
		if nestedInfo != nil {
			fabricRecordEmbeddedName(names, nestedInfo.ID)
			for _, provided := range nestedInfo.Provides {
				fabricRecordEmbeddedName(names, provided)
			}
			continue
		}

		base := jar.File
		if _, tail, ok := strings.CutLast(base, "/"); ok {
			base = tail
		}
		base = strings.TrimSuffix(base, ".jar")
		for _, dep := range depNames {
			if base == dep || strings.HasPrefix(base, dep+"-") {
				names[dep] = struct{}{}
				break
			}
		}
	}
	return names, nil
}

func fabricNestedModInfo(
	zipRdr *zip.Reader,
	filePath string,
) (*fileschema.FileFabricModIdentifier, error) {
	raw, err := readZipEntryBytes(zipRdr, filePath)
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, nil
	}

	nestedZip, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, err
	}
	return readFabricDescriptor(nestedZip)
}

func fabricRecordEmbeddedName(names map[string]struct{}, id string) {
	if id == "" {
		return
	}
	names[string(input.ToProjectName(id))] = struct{}{}
}

func fabricArtifactAuthors(authors []fileschema.FabricAuthor) []catalog.Person {
	translated := make([]catalog.Person, len(authors))
	for i, author := range authors {
		translated[i] = catalog.Person{Name: string(author)}
	}
	return translated
}

func fabricArtifactURLs(contact map[string]string) []catalog.URL {
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
			catalog.URL{
				Name: spec.name,
				Type: spec.urlType,
				URL:  url,
			},
		)
	}
	return urls
}
