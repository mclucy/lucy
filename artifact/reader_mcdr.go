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

// Read extracts artifact metadata from mcdreforged.plugin.json inside an MCDR
// plugin archive (.pyz or .mcdr).
func (r *mcdrReader) Read(
	zipRdr *zip.Reader,
	filePath string,
	resolver SlugResolver,
) ([]Info, error) {
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

		urls := make([]types.Url, 0, 1)
		if pluginInfo.Link != "" {
			urls = append(
				urls, types.Url{
					Name: "Link",
					Type: types.UrlSource,
					Url:  pluginInfo.Link,
				},
			)
		}

		info := Info{
			Ref: types.PackageRef{
				Eco:  types.EcoMcdr,
				Name: input.ToProjectName(pluginInfo.Id),
			},
			Version:  types.BareVersion(pluginInfo.Version),
			FilePath: filePath,
			Metadata: types.Metadata{
				Title:       pluginInfo.Name,
				Description: pluginInfo.Description.EnUs,
				Authors:     authors,
				Urls:        urls,
			},
		}

		if len(pluginInfo.Dependencies) > 0 {
			deps := make([]Dependency, 0, len(pluginInfo.Dependencies))
			for key, value := range pluginInfo.Dependencies {
				deps = append(
					deps, Dependency{
						Ref: types.PackageRef{
							Eco:  types.EcoMcdr,
							Name: input.ToProjectName(key),
						},
						Constraint: version.ParseRange(
							value,
							version.InferRangeDialect(types.EcoMcdr),
							types.Semver,
						),
						Mandatory: true,
					},
				)
			}
			info.Dependencies = deps
		}

		return []Info{info}, nil
	}

	return nil, nil
}
