package artifact

import (
	"archive/zip"
	"encoding/json"
	"io"

	"github.com/mclucy/lucy/dependency"
	"github.com/mclucy/lucy/exttype"
	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/types"
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
) ([]ArtifactInfo, error) {
	for _, f := range zipRdr.File {
		if f.Name != "mcdreforged.plugin.json" {
			continue
		}

		rc, err := f.Open()
		if err != nil {
			return nil, err
		}

		raw, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}

		pluginInfo := &exttype.FileMcdrPluginIdentifier{}
		if err := json.Unmarshal(raw, pluginInfo); err != nil {
			return nil, err
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

		info := ArtifactInfo{
			Ref: types.PackageRef{
				Platform: types.PlatformMCDR,
				Name:     input.ToProjectName(pluginInfo.Id),
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
			deps := make([]ArtifactDep, 0, len(pluginInfo.Dependencies))
			for key, value := range pluginInfo.Dependencies {
				deps = append(
					deps, ArtifactDep{
						Ref: types.PackageRef{
							Platform: types.PlatformMCDR,
							Name:     input.ToProjectName(key),
						},
						Constraint: dependency.ParseRange(
							value,
							dependency.InferRangeDialect(types.PlatformMCDR),
							types.Semver,
						),
						Mandatory: true,
					},
				)
			}
			info.Dependencies = deps
		}

		return []ArtifactInfo{info}, nil
	}

	return nil, nil
}
