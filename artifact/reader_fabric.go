package artifact

import (
	"archive/zip"
	"bytes"
	"encoding/json"
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
	resolver SlugResolver,
) ([]Info, error) {
	for _, f := range zipRdr.File {
		if f.Name != "fabric.mod.json" {
			continue
		}

		reader, err := f.Open()
		if err != nil {
			return nil, err
		}

		modInfo := &fileschema.FileFabricModIdentifier{}
		decodeErr := json.NewDecoder(reader).Decode(modInfo)
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

		return []Info{info}, nil
	}

	return nil, nil
}

func translateFabricArtifact(
	zipRdr *zip.Reader,
	modInfo *fileschema.FileFabricModIdentifier,
	filePath string,
) (Info, error) {
	embeddedNames, err := fabricArtifactEmbeddedModNames(zipRdr, modInfo)
	if err != nil {
		return Info{}, err
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

	return Info{
		Ref: types.PackageRef{
			Eco:  types.EcoFabric,
			Name: input.ToProjectName(modInfo.Id),
		},
		Version:      types.BareVersion(modInfo.Version),
		FilePath:     filePath,
		Dependencies: dependencies,
		Metadata: types.Metadata{
			Title:       modInfo.Name,
			Description: modInfo.Description,
			License:     modInfo.License,
			Authors:     fabricArtifactAuthors(modInfo.Authors),
			Urls:        fabricArtifactURLs(modInfo.Contact),
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
			Ref: types.PackageRef{
				Eco:  types.EcoFabric,
				Name: name,
			},
			Constraint: parseFabricArtifactVersionRanges(ranges),
			Mandatory:  mandatory,
			Type:       dependencyTypeForEmbedded(embedded),
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
		[]string(ranges),
		version.InferRangeDialect(types.EcoFabric),
		types.Semver,
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
			fabricRecordEmbeddedName(names, nestedInfo.Id)
			for _, provided := range nestedInfo.Provides {
				fabricRecordEmbeddedName(names, provided)
			}
			continue
		}

		base := jar.File
		if idx := strings.LastIndex(base, "/"); idx >= 0 {
			base = base[idx+1:]
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
		decodeErr := json.NewDecoder(reader).Decode(modInfo)
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

func fabricArtifactURLs(contact map[string]string) []types.Url {
	urlSpecs := []struct {
		key     string
		name    string
		urlType types.UrlType
	}{
		{key: "homepage", name: "Homepage", urlType: types.UrlHome},
		{key: "sources", name: "Source", urlType: types.UrlSource},
		{key: "issues", name: "Issues", urlType: types.UrlIssues},
	}

	urls := make([]types.Url, 0, len(urlSpecs))
	for _, spec := range urlSpecs {
		url := contact[spec.key]
		if url == "" {
			continue
		}
		urls = append(
			urls, types.Url{
				Name: spec.name,
				Type: spec.urlType,
				Url:  url,
			},
		)
	}
	return urls
}
