package dependency

import (
	"fmt"

	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/types"
)

// Parse is the main function to parse a RawVersion into a ComparableVersion.
//
// If the raw version is one of the special constants (which should be inferred
// before passing to this function), it returns nil.
//
// It dispatches parsing by version scheme and returns nil when parsing fails.
func Parse(
	raw types.RawVersion,
	scheme types.VersionScheme,
) types.ComparableVersion {
	switch raw {
	case types.VersionLatest, types.VersionCompatible, types.VersionNone, types.VersionAny, types.VersionUnknown:
		logger.Error(
			fmt.Errorf("attempting to parse an ambiguous version: %s", raw),
		)
		return nil
	}

	switch scheme {
	case types.Semver:
		return parseSemver(raw)
	case types.MinecraftRelease:
		return parseMinecraftRelease(raw)
	case types.MinecraftSnapshot:
		return parseMinecraftSnapshot(raw)
	default:
		return nil
	}
}
