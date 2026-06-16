package modrinth

import (
	"fmt"

	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
)

// modrinthDependencies wraps a Modrinth versionResponse for dependency
// normalization. It implements upstream.RawPackageDependencies.
type modrinthDependencies struct {
	version  *versionResponse
	platform types.PlatformId
}

var _ upstream.RawPackageDependencies = (*modrinthDependencies)(nil)

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
				Platform: m.platform,
				Name:     input.ToProjectName(m.version.Id),
			},
			Version: types.BareVersion(m.version.VersionNumber),
		}

		depId, err := DependencyToPackage(parentId, &dep)
		if err != nil {
			logger.ShowInfo(
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
