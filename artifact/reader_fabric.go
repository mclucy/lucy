package artifact

import (
	"archive/zip"
	"encoding/json"
	"io"
	"strings"

	"github.com/mclucy/lucy/dependency"
	externaltype "github.com/mclucy/lucy/exttype"
	"github.com/mclucy/lucy/syntax"
	"github.com/mclucy/lucy/tools"
	"github.com/mclucy/lucy/types"
)

type fabricReader struct{}

var _ Reader = (*fabricReader)(nil)

func newFabricReader() Reader { return &fabricReader{} }

var _ = newFabricReader

func (r *fabricReader) Read(
	zipRdr *zip.Reader,
	filePath string,
	resolver SlugResolver,
) ([]ArtifactInfo, error) {
	for _, f := range zipRdr.File {
		if f.Name != "fabric.mod.json" {
			continue
		}

		reader, err := f.Open()
		if err != nil {
			return nil, err
		}

		data, err := io.ReadAll(reader)
		if closeErr := reader.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
		if err != nil {
			return nil, err
		}

		modInfo := &externaltype.FileFabricModIdentifier{}
		if err := json.Unmarshal(data, modInfo); err != nil {
			return nil, err
		}

		return []ArtifactInfo{translateFabricArtifact(modInfo, filePath)}, nil
	}

	return nil, nil
}

func translateFabricArtifact(
	modInfo *externaltype.FileFabricModIdentifier,
	filePath string,
) ArtifactInfo {
	embeddedNames := fabricArtifactEmbeddedModNames(modInfo)
	dependencies := make(
		[]ArtifactDep, 0,
		len(modInfo.Depends)+len(modInfo.Recommends)+len(modInfo.Suggests)+
			len(modInfo.Breaks)+len(modInfo.Conflicts),
	)
	dependencies = append(
		dependencies,
		translateFabricArtifactDependencyMap(modInfo.Depends, true, false, embeddedNames)...,
	)
	dependencies = append(
		dependencies,
		translateFabricArtifactDependencyMap(modInfo.Recommends, false, false, embeddedNames)...,
	)
	dependencies = append(
		dependencies,
		translateFabricArtifactDependencyMap(modInfo.Suggests, false, false, embeddedNames)...,
	)
	dependencies = append(
		dependencies,
		translateFabricArtifactDependencyMap(modInfo.Breaks, false, true, embeddedNames)...,
	)
	dependencies = append(
		dependencies,
		translateFabricArtifactDependencyMap(modInfo.Conflicts, false, true, embeddedNames)...,
	)

	return ArtifactInfo{
		Ref: types.PackageRef{
			Platform: types.PlatformFabric,
			Name:     syntax.ToProjectName(modInfo.Id),
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
	}
}

func translateFabricArtifactDependencyMap(
	deps map[string]tools.SingleOrSlice[string],
	mandatory bool,
	inverse bool,
	embeddedNames map[string]struct{},
) []ArtifactDep {
	translated := make([]ArtifactDep, 0, len(deps))
	for id, ranges := range deps {
		name := syntax.ToProjectName(id)
		_, embedded := embeddedNames[string(name)]
		dep := ArtifactDep{
			Ref: types.PackageRef{
				Platform: types.PlatformFabric,
				Name:     name,
			},
			Constraint: parseFabricArtifactVersionRanges(ranges),
			Mandatory:  mandatory,
			Embedded:   embedded,
		}
		if inverse {
			dep.Constraint.Inverse()
		}
		translated = append(translated, dep)
	}
	return translated
}

func parseFabricArtifactVersionRanges(
	ranges tools.SingleOrSlice[string],
) types.VersionExpr {
	return dependency.ParseRanges(
		[]string(ranges),
		dependency.InferRangeDialect(types.PlatformFabric),
		types.Semver,
	)
}

func fabricArtifactEmbeddedModNames(
	modInfo *externaltype.FileFabricModIdentifier,
) map[string]struct{} {
	depNames := make([]string, 0, len(modInfo.Depends))
	for id := range modInfo.Depends {
		depNames = append(depNames, id)
	}

	names := make(map[string]struct{}, len(modInfo.Jars))
	for _, jar := range modInfo.Jars {
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
	return names
}

func fabricArtifactAuthors(authors []externaltype.FabricAuthor) []types.Person {
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
		urls = append(urls, types.Url{
			Name: spec.name,
			Type: spec.urlType,
			Url:  url,
		})
	}
	return urls
}
