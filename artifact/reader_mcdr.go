package artifact

import (
	"archive/zip"
	"encoding/json/v2"

	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/internal/fileschema"
	"github.com/mclucy/lucy/types"
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

		authors := make([]types.Person, len(pluginInfo.Author))
		for i, author := range pluginInfo.Author {
			authors[i] = types.Person{Name: author}
		}

		urls := make([]types.URL, 0, 1)
		if pluginInfo.Link != "" {
			urls = append(
				urls, types.URL{
					Name: "Link",
					Type: types.URLSource,
					URL:  pluginInfo.Link,
				},
			)
		}

		info := PackageInfo{
			Ref:      types.VersionedPackageRef{PackageRef: types.PackageRef{Name: input.ToProjectName(pluginInfo.ID), Provider: types.ProviderUnknown}, Ecosystem: types.EcosystemMCDR, Version: types.Version(pluginInfo.Version)},
			FilePath: filePath,
			Metadata: types.Metadata{
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
					types.VersionDialectMCDR,
				)
				if err != nil {
					return nil, err
				}
				deps = append(
					deps, Dependency{
						Ref:        types.VersionedPackageRef{PackageRef: types.PackageRef{Name: input.ToProjectName(key), Provider: types.ProviderUnknown}, Ecosystem: types.EcosystemMCDR},
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
