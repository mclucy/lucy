package artifact

import (
	"archive/zip"
	"bytes"
	"encoding/json/v2"
	"strings"

	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/internal/fileschema"
	"github.com/mclucy/lucy/internal/fn"
	"github.com/mclucy/lucy/types"
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
	dependencies = append(
		dependencies,
		translateFabricArtifactDependencyMap(
			modInfo.Depends,
			true,
			false,
			embeddedNames,
		)...,
	)
	dependencies = append(
		dependencies,
		translateFabricArtifactDependencyMap(
			modInfo.Recommends,
			false,
			false,
			embeddedNames,
		)...,
	)
	dependencies = append(
		dependencies,
		translateFabricArtifactDependencyMap(
			modInfo.Suggests,
			false,
			false,
			embeddedNames,
		)...,
	)
	dependencies = append(
		dependencies,
		translateFabricArtifactDependencyMap(
			modInfo.Breaks,
			false,
			true,
			embeddedNames,
		)...,
	)
	dependencies = append(
		dependencies,
		translateFabricArtifactDependencyMap(
			modInfo.Conflicts,
			false,
			true,
			embeddedNames,
		)...,
	)

	return PackageInfo{
		Ref: types.VersionedPackageRef{
			PackageRef: types.PackageRef{
				Name:     input.ToProjectName(modInfo.ID),
				Provider: types.ProviderUnknown,
			},
			Ecosystem: types.EcosystemFabric,
			Version:   types.Version(modInfo.Version),
		},
		FilePath:     filePath,
		Dependencies: dependencies,
		Metadata: types.Metadata{
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
) []Dependency {
	translated := make([]Dependency, 0, len(deps))
	for id, ranges := range deps {
		name := input.ToProjectName(id)
		_, embedded := embeddedNames[string(name)]
		dep := Dependency{
			Ref: types.VersionedPackageRef{
				PackageRef: types.PackageRef{
					Name:     name,
					Provider: types.ProviderUnknown,
				}, Ecosystem: types.EcosystemFabric,
			},
			Constraint: parseFabricArtifactVersionRanges(ranges),
			Mandatory:  mandatory,
			Origin:     dependencyOriginForEmbedded(embedded),
		}
		if inverse {
			dep.Constraint.Inverse()
		}
		translated = append(translated, dep)
	}
	return translated
}

func parseFabricArtifactVersionRanges(
	ranges fn.SingleOrSlice[string],
) types.VersionExpr {
	return version.ParseRanges(
		ranges,
		version.InferRangeDialect(types.EcosystemFabric),
		types.VersionSchemeSemver,
	)
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

func fabricArtifactAuthors(authors []fileschema.FabricAuthor) []types.Person {
	translated := make([]types.Person, len(authors))
	for i, author := range authors {
		translated[i] = types.Person{Name: string(author)}
	}
	return translated
}

func fabricArtifactURLs(contact map[string]string) []types.URL {
	urlSpecs := []struct {
		key     string
		name    string
		urlType types.URLType
	}{
		{key: "homepage", name: "Homepage", urlType: types.URLHome},
		{key: "sources", name: "Source", urlType: types.URLSource},
		{key: "issues", name: "Issues", urlType: types.URLIssues},
	}

	urls := make([]types.URL, 0, len(urlSpecs))
	for _, spec := range urlSpecs {
		url := contact[spec.key]
		if url == "" {
			continue
		}
		urls = append(
			urls, types.URL{
				Name: spec.name,
				Type: spec.urlType,
				URL:  url,
			},
		)
	}
	return urls
}
