package dependency

import (
	"strconv"
	"strings"

	"github.com/mclucy/lucy/types"
)

// Parse is the main function to parse a RawVersion into a ComparableVersion.
//
// If the raw version is one of the special constants (which should be inferred
// before passing to this function), it returns InvalidVersion.
//
// It will attempt each type of version parsing, in order of specificity.
//
// If the label is not compatible with the version, return a semantic version
// that is labeled as Raw and contains the raw version as it is. This is to
// ensure a basic support to some non-standard versions numbers by only supporting
// Eq and Neq comparisons.
func Parse(
	raw types.RawVersion,
	scheme types.VersionScheme,
) types.ComparableVersion {
	switch raw {
	case types.LatestVersion, types.LatestCompatibleVersion, types.NoVersion, types.AllVersion, types.UnknownVersion:
		return types.InvalidVersion
	}
	switch scheme {
	case types.Semver:
		return parseSemver(string(raw))
	case types.MinecraftRelease:
		return parseMinecraftRelease(string(raw))
	case types.MinecraftSnapshot:
		return parseMinecraftSnapshot(string(raw))
	default:
		return types.InvalidVersion
	}
}

func parseSemver(s string) (v types.ComparableVersion) {
	return operatorPlus(s)
}

// These two are equivalent, for now.
func parseMinecraftRelease(s string) (v types.ComparableVersion) {
	return parseSemver(s)
}

func operatorPlus(s string) (v types.ComparableVersion) {
	tokens := strings.Split(s, "+")
	if len(tokens) >= 2 {
		s = strings.Join(tokens[:len(tokens)-1], "")
	}
	v = operatorDash(s)
	if v == types.InvalidVersion {
		return v
	}
	if len(tokens) >= 2 {
		v.Build = tokens[len(tokens)-1]
	}
	return v
}

func operatorDash(s string) (v types.ComparableVersion) {
	tokens := strings.Split(s, "-")
	if len(tokens) >= 2 {
		s = strings.Join(tokens[:len(tokens)-1], "")
	}
	v = operatorDot(s)
	if v == types.InvalidVersion {
		return v
	}
	if len(tokens) >= 2 {
		v.Prerelease = tokens[len(tokens)-1]
	}
	return v
}

func operatorDot(s string) (v types.ComparableVersion) {
	tokens := strings.Split(s, ".")
	if len(tokens) >= 2 {
		major, err := strconv.Atoi(tokens[0])
		if err != nil {
			return types.InvalidVersion
		}
		minor, err := strconv.Atoi(tokens[1])
		if err != nil {
			return types.InvalidVersion
		}
		v.Major = uint16(major)
		v.Minor = uint16(minor)
	}
	if len(tokens) == 3 {
		patch, err := strconv.Atoi(tokens[2])
		if err != nil {
			return types.InvalidVersion
		}
		v.Patch = uint16(patch)
	}
	return v
}

func parseMinecraftSnapshot(s string) types.ComparableVersion {
	return operatorInWeekIndex(s)
}

func operatorWeek(s string) (v types.ComparableVersion) {
	tokens := strings.Split(s, "w")
	if len(tokens) != 2 {
		return types.InvalidVersion
	}
	major, err := strconv.Atoi(tokens[0])
	if err != nil {
		return types.InvalidVersion
	}
	minor, err := strconv.Atoi(tokens[1])
	if err != nil {
		return types.InvalidVersion
	}
	v.Major = uint16(major)
	v.Minor = uint16(minor)
	return v
}

func operatorInWeekIndex(s string) (v types.ComparableVersion) {
	tokens := s[len(s)-1]
	v = operatorWeek(s[:len(s)-1])
	if v == types.InvalidVersion {
		return v
	}
	switch tokens {
	case 'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h':
		v.Patch = uint16(tokens)
	default:
		return types.InvalidVersion
	}
	return v
}
