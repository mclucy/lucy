package modrinth

import (
	"fmt"

	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/types"
)

// modrinthDependencies wraps a Modrinth versionResponse for dependency
// normalization.
type modrinthDependencies struct {
	version  *versionResponse
	platform types.Ecosystem
}

func (m *modrinthDependencies) ToPackageDependencies() types.PackageDependencies {
	result := types.PackageDependencies{
		Authentic: false,
	}

	for _, dep := range m.version.Dependencies {
		if dep.DependencyType == incompatible {
			continue
		}

		parentId := types.VersionedPackageRef{
			Name:    input.ToProjectName(m.version.Id),
			Eco:     m.platform,
			Version: types.BareVersion(m.version.VersionNumber),
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
		result.Value = append(
			result.Value, types.Dependency{
				Id:        depId,
				Mandatory: mandatory,
			},
		)
	}

	return result
}
