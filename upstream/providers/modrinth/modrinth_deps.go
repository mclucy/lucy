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
			PackageRef: types.PackageRef{
				Eco:  m.platform,
				Name: input.ToProjectName(m.version.Id),
			},
			Version: types.BareVersion(m.version.VersionNumber),
		}

		depId, err := DependencyToPackage(parentId, &dep)
		if err != nil {
			log.ShowInfo(
				fmt.Sprintf(
					"[modrinth] skipping dependency with resolution error: %v",
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
