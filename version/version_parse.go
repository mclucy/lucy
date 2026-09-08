package version

import (
	"fmt"

	"github.com/mclucy/lucy/types"
)

// ErrAmbiguousVersion is returned when attempting to parse a non-exact version constant.
var ErrAmbiguousVersion = fmt.Errorf("attempting to parse an ambiguous version")

// Parse parses raw into a ResolvableVersion under scheme. Special version
// constants are rejected with ErrAmbiguousVersion; unknown schemes and
// unparseable values return nil.
func Parse(
	raw types.BareVersion,
	scheme types.VersionScheme,
) (types.ResolvableVersion, error) {
	switch raw {
	case types.VersionBeta, types.VersionStable, types.VersionNone, types.VersionAny, types.VersionUnknown:
		return nil, fmt.Errorf("%w: %s", ErrAmbiguousVersion, raw)
	}

	switch scheme {
	case types.Semver:
		return parseSemver(raw), nil
	case types.Maven:
		return parseMavenVersion(raw), nil
	case types.MinecraftRelease:
		return parseMinecraftRelease(raw), nil
	case types.MinecraftSnapshot:
		return parseMinecraftSnapshot(raw), nil
	default:
		return nil, nil
	}
}
