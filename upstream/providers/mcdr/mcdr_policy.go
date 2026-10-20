package mcdr

import (
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/version"
)

func selectLatestRelease(history *pluginRelease) *release {
	if history == nil || len(history.Releases) == 0 {
		return nil
	}
	return &history.Releases[history.LatestVersionIndex]
}

func selectLatestCompatibleRelease(
	history *pluginRelease,
	localMcdrVersion types.Version,
) (*release, error) {
	if history == nil {
		return nil, nil
	}

	mcdrPackage := types.VersionedPackageRef{
		Ecosystem: types.EcosystemMCDR,
		Name:      "mcdreforged",
		Version:   localMcdrVersion,
	}
	localVersion, err := version.Parse(localMcdrVersion, types.VersionSchemeSemver)
	if err != nil {
		return nil, err
	}

	for i := range history.Releases {
		rel := &history.Releases[i]
		if rel.Prerelease {
			continue
		}
		rangeExpr, ok := rel.Meta.Dependencies["mcdreforged"]
		if !ok {
			continue
		}
		dep := types.Dependency{
			ID: mcdrPackage,
			Constraint: version.ParseRange(
				rangeExpr,
				version.DialectNpmSemver,
				types.VersionSchemeSemver,
			),
			Mandatory: true,
		}
		if dep.Satisfy(mcdrPackage, localVersion) {
			return rel, nil
		}
	}

	return nil, nil
}
