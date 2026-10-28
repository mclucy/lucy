package modrinth

import (
	"context"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/upstream"
)

// Builds lists every version published by a Modrinth project. project accepts
// either a slug or a project ID; the project version endpoint resolves both.
// The list is unfiltered: no game version, loader, channel, or status is
// excluded.
func (provider) Builds(
	ctx context.Context,
	project string,
) ([]upstream.Build, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	versions, err := listVersions(catalog.Name(project))
	if err != nil {
		return nil, err
	}

	builds := make([]upstream.Build, len(versions))
	for i, v := range versions {
		builds[i] = upstream.Build{
			Version:      v.VersionNumber,
			GameVersions: v.GameVersions,
			Loaders:      v.Loaders,
			Channel:      string(upstream.ParseReleaseChannel(v.VersionType)),
			Published:    v.DatePublished,
		}
	}
	return builds, nil
}
