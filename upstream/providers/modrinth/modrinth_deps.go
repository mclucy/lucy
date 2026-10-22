package modrinth

import (
	"fmt"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/upstream"
	versionpkg "github.com/mclucy/lucy/version"
)

// modrinthDependencies wraps a Modrinth versionResponse for dependency
// normalization.
type modrinthDependencies struct {
	version  *versionResponse
	platform catalog.Ecosystem
}

func (m *modrinthDependencies) ToDependencySet() upstream.DependencySet {
	result := upstream.DependencySet{
		FromArtifact: false,
	}

	for _, dep := range m.version.Dependencies {
		if dep.DependencyType == incompatible {
			continue
		}

		parentId := catalog.VersionedRef{
			Name:      input.ToProjectName(m.version.ID),
			Ecosystem: m.platform,
			Version:   versionpkg.Version(m.version.VersionNumber),
		}

		depId, err := DependencyToPackage(parentId, &dep)
		if err != nil {
			log.Warn(
				fmt.Errorf(
					"modrinth: skipping dependency with resolution error: %w",
					err,
				),
			)
			continue
		}

		mandatory := dep.DependencyType == required
		result.Dependencies = append(
			result.Dependencies, catalog.Dependency{
				ID:        depId,
				Mandatory: mandatory,
			},
		)
	}

	return result
}
