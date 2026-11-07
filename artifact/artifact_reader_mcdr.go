package artifact

import (
	"archive/zip"
	"encoding/json/v2"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/internal/fileschema"
	"github.com/mclucy/lucy/version"
)

type mcdrReader struct{}

func newMcdrReader() Reader {
	return &mcdrReader{}
}

func (r *mcdrReader) Read(
	zipRdr *zip.Reader,
	filePath string,
) ([]PackageInfo, error) {
	for _, f := range zipRdr.File {
		if f.Name != "mcdreforged.plugin.json" {
			continue
		}

		rc, err := f.Open()
		if err != nil {
			return nil, err
		}

		pluginInfo := &fileschema.FileMcdrPluginIdentifier{}
		decodeErr := json.UnmarshalRead(rc, pluginInfo)
		closeErr := rc.Close()
		if decodeErr != nil {
			return nil, decodeErr
		}
		if closeErr != nil {
			return nil, closeErr
		}

		authors := make([]catalog.Person, len(pluginInfo.Author))
		for i, author := range pluginInfo.Author {
			authors[i] = catalog.Person{Name: author}
		}

		urls := make([]catalog.URL, 0, 1)
		if pluginInfo.Link != "" {
			urls = append(
				urls, catalog.URL{
					Name: "Link",
					Type: catalog.URLSource,
					URL:  pluginInfo.Link,
				},
			)
		}

		info := PackageInfo{
			Ref:      catalog.VersionedRef{Ref: catalog.Ref{Name: input.ToProjectName(pluginInfo.ID), Provider: catalog.ProviderUnknown}, Ecosystem: catalog.EcosystemMCDR, Version: version.Version(pluginInfo.Version)},
			FilePath: filePath,
			Metadata: catalog.Metadata{
				Title:       pluginInfo.Name,
				Description: pluginInfo.Description.EnUs,
				Authors:     authors,
				URLs:        urls,
			},
		}

		if len(pluginInfo.Dependencies) > 0 {
			deps := make([]Dependency, 0, len(pluginInfo.Dependencies))
			for key, value := range pluginInfo.Dependencies {
				constraint, err := version.ParseConstraint(
					value,
					version.DialectMCDR,
				)
				if err != nil {
					return nil, err
				}
				deps = append(
					deps, Dependency{
						Ref:        catalog.VersionedRef{Ref: catalog.Ref{Name: input.ToProjectName(key), Provider: catalog.ProviderUnknown}, Ecosystem: catalog.EcosystemMCDR},
						Constraint: constraint,
						Mandatory:  true,
					},
				)
			}
			info.Dependencies = deps
		}

		return []PackageInfo{info}, nil
	}

	return nil, nil
}
