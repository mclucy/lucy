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

var _ = newFabricReader

func (r *fabricReader) Read(
	zipRdr *zip.Reader,
	filePath string,
) ([]PackageInfo, error) {
	for _, f := range zipRdr.File {
		if f.Name != "fabric.mod.json" {
			continue
		}

		reader, err := f.Open()
		if err != nil {
			return nil, err
		}

		modInfo := &fileschema.FileFabricModIdentifier{}
		decodeErr := json.UnmarshalRead(reader, modInfo)
		closeErr := reader.Close()
		if decodeErr != nil {
			return nil, decodeErr
		}
		if closeErr != nil {
			return nil, closeErr
		}

		info, err := translateFabricArtifact(zipRdr, modInfo, filePath)
		if err != nil {
			return nil, err
		}

		return []PackageInfo{info}, nil
	}

	return nil, nil
}

func translateFabricArtifact(
	zipRdr *zip.Reader,
	modInfo *fileschema.FileFabricModIdentifier,
	filePath string,
) (PackageInfo, error) {
	embeddedNames, err := fabricArtifactEmbeddedModNames(zipRdr, modInfo)
	if err != nil {
		return PackageInfo{}, err
	}
	dependencies := make(
		[]Dependency, 0,
		len(modInfo.Depends)+len(modInfo.Recommends)+len(modInfo.Suggests)+
			len(modInfo.Breaks)+len(modInfo.Conflicts),
	)
	for _, group := range []struct {
		dependencies map[string]fn.SingleOrSlice[string]
		mandatory    bool
		inverse      bool
	}{
		{modInfo.Depends, true, false},
		{modInfo.Recommends, false, false},
		{modInfo.Suggests, false, false},
		{modInfo.Breaks, false, true},
		{modInfo.Conflicts, false, true},
	} {
		translated, err := translateFabricArtifactDependencyMap(
			group.dependencies,
			group.mandatory,
			group.inverse,
			embeddedNames,
		)
		if err != nil {
			return PackageInfo{}, err
		}
		dependencies = append(dependencies, translated...)
	}

	return PackageInfo{
		Ref: catalog.VersionedRef{
			Ref: catalog.Ref{
				Name:     input.ToProjectName(modInfo.ID),
				Provider: catalog.ProviderUnknown,
			},
			Ecosystem: catalog.EcosystemFabric,
			Version:   version.Version(modInfo.Version),
		},
		FilePath:     filePath,
		Dependencies: dependencies,
		Metadata: catalog.Metadata{
			Title:       modInfo.Name,
			Description: modInfo.Description,
			License:     modInfo.License,
			Authors:     fabricArtifactAuthors(modInfo.Authors),
			URLs:        fabricArtifactURLs(modInfo.Contact),
		},
	}, nil
}

func translateFabricArtifactDependencyMap(
	deps map[string]fn.SingleOrSlice[string],
	mandatory bool,
	inverse bool,
	embeddedNames map[string]struct{},
) ([]Dependency, error) {
	translated := make([]Dependency, 0, len(deps))
	for id, ranges := range deps {
		name := input.ToProjectName(id)
		_, embedded := embeddedNames[string(name)]
		constraint, err := parseFabricArtifactVersionRanges(ranges)
		if err != nil {
			return nil, err
		}
		dep := Dependency{
			Ref: catalog.VersionedRef{
				Ref: catalog.Ref{
					Name:     name,
					Provider: catalog.ProviderUnknown,
				}, Ecosystem: catalog.EcosystemFabric,
			},
			Constraint: constraint,
			Mandatory:  mandatory,
			Origin:     dependencyOriginForEmbedded(embedded),
		}
		if inverse {
			dep.Constraint = dep.Constraint.Negate()
		}
		translated = append(translated, dep)
	}
	return translated, nil
}

func parseFabricArtifactVersionRanges(
	ranges fn.SingleOrSlice[string],
) (version.Expr, error) {
	return version.ParseConstraints(ranges, version.DialectFabric)
}

func fabricArtifactEmbeddedModNames(
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
	raw, err := readZipEntry(zipRdr, filePath)
	if err != nil || raw == nil {
		return nil, err
	}

	nestedZip, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, err
	}

	for _, f := range nestedZip.File {
		if f.Name != "fabric.mod.json" {
			continue
		}
		reader, err := f.Open()
		if err != nil {
			return nil, err
		}
		modInfo := &fileschema.FileFabricModIdentifier{}
		decodeErr := json.UnmarshalRead(reader, modInfo)
		closeErr := reader.Close()
		if decodeErr != nil {
			return nil, decodeErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		return modInfo, nil
	}

	return nil, nil
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
			urls, catalog.URL{
				Name: spec.name,
				Type: spec.urlType,
				URL:  url,
			},
		)
	}
	return urls
}
