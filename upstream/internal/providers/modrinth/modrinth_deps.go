package modrinth

import (
	"context"
	"errors"
	"fmt"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/upstream"
	versionpkg "github.com/mclucy/lucy/version"
)

// modrinthDependencies wraps a Modrinth versionResponse for dependency
// normalization.
type modrinthDependencies struct {
	provider *Provider
	version  *versionResponse
	platform catalog.Ecosystem
}

func (m *modrinthDependencies) ToDependencySet(
	ctx context.Context,
) (upstream.DependencySet, error) {
	result := upstream.DependencySet{
		FromArtifact: false,
	}

	for _, dep := range m.version.Dependencies {
		if err := ctx.Err(); err != nil {
			return upstream.DependencySet{}, err
		}
		if dep.DependencyType == incompatible {
			continue
		}

		parentId := catalog.VersionedRef{
			Name:      input.ToProjectName(m.version.ID),
			Ecosystem: m.platform,
			Version:   versionpkg.Version(m.version.VersionNumber),
		}

		depId, err := m.provider.DependencyToPackage(ctx, parentId, &dep)
		if err != nil {
			if ctx.Err() != nil {
				return upstream.DependencySet{}, ctx.Err()
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return upstream.DependencySet{}, err
			}
			m.provider.logger.Warn(
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

	if err := ctx.Err(); err != nil {
		return upstream.DependencySet{}, err
	}
	return result, nil
}
