package dependency

import (
	"strconv"

	"github.com/mclucy/lucy/types"
)

const (
	maxSnapshotWeek      = 54
	maxSnapshotIndex     = uint8('h')
	minSnapshotIndex     = uint8('a')
	minPost26ReleaseYear = 26
)

// docs:
// https://zh.minecraft.wiki/w/%E7%89%88%E6%9C%AC%E6%A0%BC%E5%BC%8F
// https://www.minecraft.net/en-us/article/minecraft-new-version-numbering-system

type Pre26MinecraftSnapshotVersion struct {
	Year      uint8 // the last two digits of the year, e.g. 24 for 2024
	WorkCycle uint8 // the week of the year
	Index     uint8 // the letter, stored as ASCII values directly
}

func (v1 *Pre26MinecraftSnapshotVersion) Compare(v2 types.ComparableVersion) (
	int,
	bool,
) {
	o, ok := v2.(*Pre26MinecraftSnapshotVersion)
	if !ok || v1 == nil || o == nil {
		return 0, false
	}
	if cmp := compareUint8(v1.Year, o.Year); cmp != 0 {
		return cmp, true
	}
	if cmp := compareUint8(v1.WorkCycle, o.WorkCycle); cmp != 0 {
		return cmp, true
	}
	return compareUint8(v1.Index, o.Index), true
}

func (v1 *Pre26MinecraftSnapshotVersion) Validate() bool {
	if v1 == nil {
		return false
	}
	return v1.Year != 0 &&
		v1.WorkCycle > 0 && v1.WorkCycle <= maxSnapshotWeek &&
		v1.Index >= minSnapshotIndex && v1.Index <= maxSnapshotIndex
}

func (v1 *Pre26MinecraftSnapshotVersion) Scheme() types.VersionScheme {
	return types.MinecraftSnapshot
}

// Post26MinecraftSnapshotVersion represents snapshots after the new numbering
// scheme (for example 26.1-snapshot-1).
type Post26MinecraftSnapshotVersion struct {
	Year      uint8
	Update    uint8
	SnapshotN uint8
}

func (v1 *Post26MinecraftSnapshotVersion) Compare(v2 types.ComparableVersion) (
	int,
	bool,
) {
	o, ok := v2.(*Post26MinecraftSnapshotVersion)
	if !ok || v1 == nil || o == nil {
		return 0, false
	}
	if cmp := compareUint8(v1.Year, o.Year); cmp != 0 {
		return cmp, true
	}
	if cmp := compareUint8(v1.Update, o.Update); cmp != 0 {
		return cmp, true
	}
	return compareUint8(v1.SnapshotN, o.SnapshotN), true
}

func (v1 *Post26MinecraftSnapshotVersion) Validate() bool {
	if v1 == nil {
		return false
	}
	return v1.Year >= minPost26ReleaseYear &&
		v1.Update > 0 &&
		v1.SnapshotN > 0
}

func (v1 *Post26MinecraftSnapshotVersion) Scheme() types.VersionScheme {
	return types.MinecraftSnapshot
}

type MinecraftVersion struct {
	Year             uint8
	Update           uint8
	Hotfix           uint8
	Prerelease       PrereleaseType
	PrereleaseNumber uint8
	Post26           bool
}

type PrereleaseType string

const (
	Post26Prerelease       PrereleaseType = "pre"
	Post26ReleaseCandidate PrereleaseType = "rc"
)

func (v *MinecraftVersion) Title() string {
	if v == nil {
		return ""
	}
	if v.Post26 {
		base := strconv.Itoa(int(v.Year)) + "." + strconv.Itoa(int(v.Update))
		if v.Prerelease != "" {
			return base + "-" + string(v.Prerelease) + "-" + strconv.Itoa(int(v.PrereleaseNumber))
		}
		if v.Hotfix > 0 {
			return base + "." + strconv.Itoa(int(v.Hotfix))
		}
		return base
	}
	base := strconv.Itoa(int(v.Year)) + "." + strconv.Itoa(int(v.Update))
	if v.Hotfix > 0 {
		base += "." + strconv.Itoa(int(v.Hotfix))
	}
	if v.Prerelease != "" {
		return base + "-" + string(v.Prerelease) + strconv.Itoa(int(v.PrereleaseNumber))
	}
	return base
}

func (v1 *MinecraftVersion) Compare(v2 types.ComparableVersion) (int, bool) {
	o, ok := v2.(*MinecraftVersion)
	if !ok || v1 == nil || o == nil {
		return 0, false
	}
	if v1.Post26 != o.Post26 {
		return 0, false
	}
	if cmp := compareUint8(v1.Year, o.Year); cmp != 0 {
		return cmp, true
	}
	if cmp := compareUint8(v1.Update, o.Update); cmp != 0 {
		return cmp, true
	}
	if cmp := compareUint8(v1.Hotfix, o.Hotfix); cmp != 0 {
		return cmp, true
	}
	if cmp := compareUint8(
		prereleaseRank(v1.Prerelease),
		prereleaseRank(o.Prerelease),
	); cmp != 0 {
		return cmp, true
	}
	return compareUint8(v1.PrereleaseNumber, o.PrereleaseNumber), true
}

func (v1 *MinecraftVersion) Validate() bool {
	if v1 == nil {
		return false
	}
	if v1.Year == 0 || v1.Update == 0 {
		return false
	}
	if v1.Post26 && v1.Year < minPost26ReleaseYear {
		return false
	}
	switch v1.Prerelease {
	case "":
		return true
	case Post26Prerelease, Post26ReleaseCandidate:
		if v1.PrereleaseNumber == 0 {
			return false
		}
		if v1.Post26 {
			return v1.Hotfix == 0
		}
		return true
	default:
		return false
	}
}

func (v1 *MinecraftVersion) Scheme() types.VersionScheme {
	return types.MinecraftRelease
}

func prereleaseRank(pr PrereleaseType) uint8 {
	switch pr {
	case Post26Prerelease:
		return 0
	case Post26ReleaseCandidate:
		return 1
	default:
		return 2
	}
}

func compareUint8(a, b uint8) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
