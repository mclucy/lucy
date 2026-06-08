package dependency

import (
	"fmt"

	"github.com/mclucy/lucy/types"
)

// ErrAmbiguousVersion is returned when attempting to parse an ambiguous version constant.
var ErrAmbiguousVersion = fmt.Errorf("attempting to parse an ambiguous version")

// Parse is the main function to parse a BareVersion into a ResolvableVersion.
//
// If the raw version is one of the special constants (which should be inferred
// before passing to this function), it returns an error.
//
// It dispatches parsing by version scheme and returns nil when parsing fails.
func Parse(
	raw types.BareVersion,
	scheme types.VersionScheme,
) (types.ResolvableVersion, error) {
	switch raw {
	case types.VersionLatest, types.VersionCompatible, types.VersionNone, types.VersionAny, types.VersionUnknown:
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
