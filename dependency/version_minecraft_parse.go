package dependency

import (
	"strconv"
	"strings"

	"github.com/mclucy/lucy/types"
)

func parseMinecraftSnapshot(s types.RawVersion) types.ComparableVersion {
	if v := parsePre26WeekSnapshot(s); v != nil {
		return v
	}
	return parsePost26MinecraftSnapshot(s)
}

func parsePre26WeekSnapshot(s types.RawVersion) types.ComparableVersion {
	if len(s) < 4 {
		return nil
	}
	index := s[len(s)-1]
	v := parseSnapshotWorkCycle(s[:len(s)-1])
	if v == nil {
		return nil
	}
	if index < minSnapshotIndex || index > maxSnapshotIndex {
		return nil
	}
	v.Index = index
	if !v.Validate() {
		return nil
	}
	return v
}

func parseSnapshotWorkCycle(s types.RawVersion) *Pre26MinecraftSnapshotVersion {
	tokens := strings.Split(string(s), "w")
	if len(tokens) != 2 {
		return nil
	}
	year, ok := parseUint8(tokens[0])
	if !ok {
		return nil
	}
	workCycle, ok := parseUint8(tokens[1])
	if !ok {
		return nil
	}
	return &Pre26MinecraftSnapshotVersion{
		Year:      year,
		WorkCycle: workCycle,
		Index:     0,
	}
}

func parsePost26MinecraftSnapshot(s types.RawVersion) types.ComparableVersion {
	str := string(s)
	if !strings.Contains(str, "-snapshot-") {
		return nil
	}
	tokens := strings.Split(str, "-snapshot-")
	if len(tokens) != 2 {
		return nil
	}
	core := strings.Split(tokens[0], ".")
	if len(core) != 2 {
		return nil
	}
	year, ok := parseUint8(core[0])
	if !ok {
		return nil
	}
	update, ok := parseUint8(core[1])
	if !ok {
		return nil
	}
	snapshotN, ok := parseUint8(tokens[1])
	if !ok {
		return nil
	}
	v := &Post26MinecraftSnapshotVersion{
		Year:      year,
		Update:    update,
		SnapshotN: snapshotN,
	}
	if !v.Validate() {
		return nil
	}
	return v
}

func parseMinecraftRelease(s types.RawVersion) types.ComparableVersion {
	if v := parsePost26MinecraftRelease(s); v != nil {
		return v
	}
	return parsePre26MinecraftRelease(s)
}

func parsePost26MinecraftRelease(s types.RawVersion) types.ComparableVersion {
	str := string(s)
	if str == "" || strings.Contains(str, "-snapshot-") {
		return nil
	}
	core, suffix := splitCoreAndSuffix(str)

	coreTokens := strings.Split(core, ".")
	if len(coreTokens) < 2 || len(coreTokens) > 3 {
		return nil
	}
	year, ok := parseUint8(coreTokens[0])
	if !ok || year < minPost26ReleaseYear {
		return nil
	}
	update, ok := parseUint8(coreTokens[1])
	if !ok {
		return nil
	}
	v := &MinecraftVersion{
		Year:   year,
		Update: update,
		Post26: true,
	}
	if len(coreTokens) == 3 {
		hotfix, ok := parseUint8(coreTokens[2])
		if !ok {
			return nil
		}
		v.Hotfix = hotfix
	}
	if suffix != "" {
		prerelease, number, ok := parseMinecraftPrereleaseSuffix(suffix)
		if !ok {
			return nil
		}
		v.Prerelease = prerelease
		v.PrereleaseNumber = number
	}
	if !v.Validate() {
		return nil
	}
	return v
}

func parsePre26MinecraftRelease(s types.RawVersion) types.ComparableVersion {
	str := string(s)
	if str == "" {
		return nil
	}
	core, suffix := splitCoreAndSuffix(str)

	coreTokens := strings.Split(core, ".")
	if len(coreTokens) < 2 || len(coreTokens) > 3 {
		return nil
	}
	year, ok := parseUint8(coreTokens[0])
	if !ok || year >= minPost26ReleaseYear {
		return nil
	}
	update, ok := parseUint8(coreTokens[1])
	if !ok {
		return nil
	}
	v := &MinecraftVersion{
		Year:   year,
		Update: update,
		Post26: false,
	}
	if len(coreTokens) == 3 {
		hotfix, ok := parseUint8(coreTokens[2])
		if !ok {
			return nil
		}
		v.Hotfix = hotfix
	}
	if suffix != "" {
		prerelease, number, ok := parseMinecraftPrereleaseSuffix(suffix)
		if !ok {
			return nil
		}
		v.Prerelease = prerelease
		v.PrereleaseNumber = number
	}
	if !v.Validate() {
		return nil
	}
	return v
}

func splitCoreAndSuffix(raw string) (core string, suffix string) {
	core = raw
	if idx := strings.IndexByte(raw, '-'); idx >= 0 {
		core = raw[:idx]
		suffix = raw[idx+1:]
	}
	return core, suffix
}

func parseMinecraftPrereleaseSuffix(suffix string) (
	PrereleaseType,
	uint8,
	bool,
) {
	if strings.HasPrefix(suffix, "pre") {
		number, ok := parsePrereleaseNumber(strings.TrimPrefix(suffix, "pre"))
		if !ok {
			return "", 0, false
		}
		return Post26Prerelease, number, true
	}
	if strings.HasPrefix(suffix, "rc") {
		number, ok := parsePrereleaseNumber(strings.TrimPrefix(suffix, "rc"))
		if !ok {
			return "", 0, false
		}
		return Post26ReleaseCandidate, number, true
	}
	return "", 0, false
}

func parsePrereleaseNumber(s string) (uint8, bool) {
	s = strings.TrimPrefix(s, "-")
	if s == "" {
		return 0, false
	}
	n, ok := parseUint8(s)
	if !ok || n == 0 {
		return 0, false
	}
	return n, true
}

func parseUint8(s string) (uint8, bool) {
	if s == "" {
		return 0, false
	}
	v, err := strconv.Atoi(s)
	if err != nil || v < 0 || v > 255 {
		return 0, false
	}
	return uint8(v), true
}
