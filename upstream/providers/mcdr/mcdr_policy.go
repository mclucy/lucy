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
	localMcdrVersion types.BareVersion,
) (*release, error) {
	if history == nil {
		return nil, nil
	}

	mcdrPackage := types.VersionedPackageRef{
		Eco:     types.EcoMcdr,
		Name:    "mcdreforged",
		Version: localMcdrVersion,
	}
	localVersion, err := version.Parse(localMcdrVersion, types.Semver)
	if err != nil {
		return nil, err
	}

	for i := range history.Releases {
		rel := &history.Releases[i]
		rangeExpr, ok := rel.Meta.Dependencies["mcdreforged"]
		if !ok {
			continue
		}
		dep := types.Dependency{
			Id: mcdrPackage,
			Constraint: version.ParseRange(
				rangeExpr,
				version.DialectNpmSemver,
				types.Semver,
			),
			Mandatory: true,
		}
		if dep.Satisfy(mcdrPackage, localVersion) {
			return rel, nil
		}
	}

	return nil, nil
}
