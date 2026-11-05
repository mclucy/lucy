package mcdr

import (
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
	localMcdrVersion version.Version,
) (*release, error) {
	if history == nil {
		return nil, nil
	}

	dialect, err := version.NewEngine(version.DialectMCDR)
	if err != nil {
		return nil, err
	}
	localVersion, err := dialect.ParseVersion(localMcdrVersion)
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
		constraint, err := dialect.ParseConstraint(rangeExpr)
		if err != nil {
			return nil, err
		}
		if dialect.Match(constraint, localVersion) {
			return rel, nil
		}
	}

	return nil, nil
}
