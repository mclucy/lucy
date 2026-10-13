package version

import (
	"fmt"

	"github.com/mclucy/lucy/types"
)

// ErrAmbiguousVersion is returned when attempting to parse a non-exact version constant.
var ErrAmbiguousVersion = fmt.Errorf("attempting to parse an ambiguous version")

// Parse rejects selectors, unsupported schemes, and invalid concrete versions.
func Parse(
	raw types.BareVersion,
	scheme types.VersionScheme,
) (types.ResolvableVersion, error) {
	switch raw {
	case types.VersionBeta, types.VersionStable, types.VersionNone, types.VersionAny, types.VersionUnknown:
		return nil, fmt.Errorf("%w: %s", ErrAmbiguousVersion, raw)
	}

	var parsed types.ResolvableVersion
	switch scheme {
	case types.Semver:
		parsed = parseSemver(raw)
	case types.Maven:
		parsed = parseMavenVersion(raw)
	case types.MinecraftRelease:
		parsed = parseMinecraftRelease(raw)
	case types.MinecraftSnapshot:
		parsed = parseMinecraftSnapshot(raw)
	default:
		return nil, fmt.Errorf("unsupported version scheme %v", scheme)
	}
	if parsed == nil {
		return nil, fmt.Errorf("invalid concrete version %q for scheme %v", raw, scheme)
	}
	return parsed, nil
}
