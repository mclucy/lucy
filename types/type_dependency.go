package types

import (
	"strings"

	"github.com/mclucy/lucy/tools"
)

// RawVersion is the version of a package. Here we expect mods and plugins
// use semver (which they should). A known exception is Minecraft snapshots.
//
// There are several special constants for a ambiguous(adaptive) version.
// You MUST call remote.InferVersion() before parsing them to ComparableVersion.
type RawVersion string

func (v RawVersion) String() string {
	switch v {
	case AllVersion, "":
		return "any"
	case NoVersion:
		return "none"
	case UnknownVersion:
		return "unknown"
	case LatestVersion:
		return "latest"
	case LatestCompatibleVersion:
		return "compatible"
	}
	return string(v)
}

func (v RawVersion) NeedsInfer() bool {
	switch v {
	case AllVersion, NoVersion, UnknownVersion, LatestVersion, LatestCompatibleVersion:
		return true
	}
	return false
}

var (
	AllVersion              RawVersion = "all"
	NoVersion               RawVersion = "none"
	UnknownVersion          RawVersion = "unknown"
	LatestVersion           RawVersion = "latest"
	LatestCompatibleVersion RawVersion = "compatible"
)

func (p1 ComparableVersion) schemeMatch(p2 ComparableVersion) bool {
	return p1.Scheme == p2.Scheme
}

// Eq checks whether p1 is equal to p2.
func (p1 ComparableVersion) Eq(p2 ComparableVersion) bool {
	if !p1.schemeMatch(p2) {
		return false
	}
	return p1.Major == p2.Major && p1.Minor == p2.Minor && p1.Patch == p2.Patch
}

// StrictEq checks whether p1 is strictly equal to p2. This includes
// the prerelease tag.
func (p1 ComparableVersion) StrictEq(p2 ComparableVersion) bool {
	if !p1.schemeMatch(p2) {
		return false
	}
	// Even in strict equality, we ignore the build.
	if p1.Scheme != p2.Scheme {
		return false
	}
	return p1.Eq(p2)
}

func (p1 ComparableVersion) WeakEq(p2 ComparableVersion) bool {
	if p1.Minor == 0 && p1.Patch == 0 {
		// if a minor is not specified, only compare major
		return p1.Major == p2.Major
	} else {
		// if a minor is specified, only ignore patch
		return p1.Major == p2.Major && p1.Minor == p2.Minor
	}
}

// Neq checks whether p1 is not equal to p2.
func (p1 ComparableVersion) Neq(p2 ComparableVersion) bool {
	if !p1.schemeMatch(p2) {
		return false
	}
	return !p1.Eq(p2)
}

// Lt checks whether p1 is less than p2.
func (p1 ComparableVersion) Lt(p2 ComparableVersion) bool {
	if !p1.schemeMatch(p2) {
		return false
	}

	if p1.Major < p2.Major {
		return true
	} else if p1.Major > p2.Major {
		return false
	}
	if p1.Minor < p2.Minor {
		return true
	} else if p1.Minor > p2.Minor {
		return false
	}
	if p1.Patch < p2.Patch {
		return true
	}

	// Usually a version with a prerelease tag is considered lower than
	// the same version without a prerelease tag.
	if p1.Prerelease == "" {
		if p2.Prerelease != "" {
			return true
		}
		return false
	} else {
		if p2.Prerelease != "" {
			// lexicographical order
			return strings.Compare(p1.Prerelease, p2.Prerelease) < 0
		} else {
			// p1 has a prerelease tag, p2 does not.
			return false
		}
	}
}

// Gt checks whether p1 is greater than p2.
func (p1 ComparableVersion) Gt(p2 ComparableVersion) bool {
	if !p1.schemeMatch(p2) {
		return false
	}

	if p1.Major > p2.Major {
		return true
	} else if p1.Major < p2.Major {
		return false
	}
	if p1.Minor > p2.Minor {
		return true
	} else if p1.Minor < p2.Minor {
		return false
	}
	if p1.Patch > p2.Patch {
		return true
	}

	// Usually a version with a prerelease tag is considered lower than
	// the same version without a prerelease tag.
	if p1.Prerelease == "" {
		if p2.Prerelease != "" {
			return false
		}
		return false
	} else {
		if p2.Prerelease != "" {
			// lexicographical order
			return strings.Compare(p1.Prerelease, p2.Prerelease) > 0
		} else {
			// p1 has a prerelease tag, p2 does not.
			return true
		}
	}
}

// WeakGt is for being compatible with the '^' operator in semver. Like Gt,
// it checks whether p1 is greater than p2. However, it does not allow
// the major version to be different.
func (p1 ComparableVersion) WeakGt(p2 ComparableVersion) bool {
	if !p1.schemeMatch(p2) {
		return false
	}

	if p1.Major != p2.Major {
		return false
	}
	return p1.Gt(p2)
}

// Lte checks whether p1 is less than or equal to p2.
func (p1 ComparableVersion) Lte(p2 ComparableVersion) bool {
	if !p1.schemeMatch(p2) {
		return false
	}

	return p1.Lt(p2) || p1.Eq(p2)
}

// Gte checks whether p1 is greater than or equal to p2.
func (p1 ComparableVersion) Gte(p2 ComparableVersion) bool {
	if !p1.schemeMatch(p2) {
		return false
	}

	return p1.Gt(p2) || p1.Eq(p2)
}

// ComparableVersion is a structural representation of a version (in numbers and
// partial strings). Note that it do not include the package name.
//
// You can see it as a RawVersion that has been parsed, and with ambiguities
// resolved.
//
// For Minecraft Snapshots, Major is the year, Minor is the week of the year,
// and Patch is the rune at the end of the version string (to ascii code).
//
// In principle, you cannot compare two versions with different schema. This
// type of comparison always returns false.
//
// The StrictEq method is checks for Prerelease.
//
// Build is for recognition purposes only. It is not used in any conditional expressions.
//
// Patch is allowed to be zero for Minecraft releases, by this I mean the first
// release of each Minor, such as 1.19.
type ComparableVersion struct {
	Scheme     VersionScheme // The type of versioning scheme used.
	Major      uint16
	Minor      uint16
	Patch      uint16
	Prerelease string
	Build      string
}

type VersionScheme uint8

const (
	Semver VersionScheme = iota

	// MinecraftSnapshot Docs
	// https://zh.minecraft.wiki/w/%E7%89%88%E6%9C%AC%E6%A0%BC%E5%BC%8F#%E5%BF%AB%E7%85%A7%EF%BC%88Snapshot%EF%BC%89
	MinecraftSnapshot
	MinecraftRelease

	Invalid
)

var InvalidVersion = ComparableVersion{
	Scheme: Invalid,
}

func (v ComparableVersion) Validate() bool {
	switch v.Scheme {
	case Semver:
		return v.Major != 0 || v.Minor != 0 || v.Patch != 0
	case MinecraftSnapshot:
		return v.Major != 0 && // year
			v.Minor > 0 && v.Minor <= maxWeek && // week (work cycle)
			v.Patch >= minSnapshotIndex && v.Patch <= maxSnapshotIndex // in-week index (as ascii code)
	case MinecraftRelease:
		return v.Major != 0 && v.Minor != 0
	case Invalid:
		return false
	default:
		return false

	}
}

const (
	maxWeek          uint16 = 52 + 2
	maxSnapshotIndex        = uint16('h')
	minSnapshotIndex        = uint16('a')
)

// Dependency represents a dependency requirement for a package.
//
// DO NOT read the Id.Version field. It is supposed to be empty.
//
// Dependency.Constraint is a 2d-array. The outer array were evaluated with OR,
// while the inner array were evaluated with AND. While it is nil or empty, it
// means there is no constraint (all versions are acceptable).
type Dependency struct {
	Id         PackageId
	Constraint VersionConstraintExpression
	Mandatory  bool
}

type VersionConstraintExpression [][]VersionConstraint

type VersionConstraint struct {
	Value    ComparableVersion
	Operator VersionOperator
}

// Inverse inverts the version constraint expression.
// This functions is in-place.
func (exps VersionConstraintExpression) Inverse() VersionConstraintExpression {
	tools.ForEachOnMatrix(
		exps,
		func(exp VersionConstraint) { exp.Inverse() },
	)
	return exps
}

// Inverse inverts the version constraint.
// This function is in-place.
func (exp *VersionConstraint) Inverse() {
	switch exp.Operator {
	case OpEq:
		exp.Operator = OpNeq
	case OpNeq:
		exp.Operator = OpEq
	case OpGt:
		exp.Operator = OpLte
	case OpWeakGt:
		exp.Operator = OpLte
	case OpGte:
		exp.Operator = OpLt
	case OpLt:
		exp.Operator = OpGte
	case OpLte:
		exp.Operator = OpGt
	}
}

func (d Dependency) Satisfy(
	id PackageId,
	v ComparableVersion,
) bool {
	if (id.Platform != d.Id.Platform) || (id.Name != d.Id.Name) {
		return false
	}

	if d.Constraint == nil || tools.IsEmptyVector(d.Constraint) {
		return true
	}

	for _, orStatements := range d.Constraint {
		satisfied := true
		for _, andStatements := range orStatements {
			if !andStatements.Operator.Comparator()(v, andStatements.Value) {
				satisfied = false
				break
			}
		}
		if satisfied {
			return true
		}
	}
	return false
}

type VersionOperator uint8

type versionComparator func(p1, p2 ComparableVersion) bool

var operatorFunctions = map[VersionOperator]versionComparator{
	OpEq:     func(p1, p2 ComparableVersion) bool { return p1.Eq(p2) },
	OpWeakEq: func(p1, p2 ComparableVersion) bool { return p1.WeakEq(p2) },
	OpNeq:    func(p1, p2 ComparableVersion) bool { return p1.Neq(p2) },
	OpGt:     func(p1, p2 ComparableVersion) bool { return p1.Gt(p2) },
	OpWeakGt: func(p1, p2 ComparableVersion) bool { return p1.WeakGt(p2) },
	OpGte:    func(p1, p2 ComparableVersion) bool { return p1.Gte(p2) },
	OpLt:     func(p1, p2 ComparableVersion) bool { return p1.Lt(p2) },
	OpLte:    func(p1, p2 ComparableVersion) bool { return p1.Lte(p2) },
}

const (
	OpEq     VersionOperator = iota
	OpWeakEq                 // for ~ operator in semver
	OpNeq
	OpGt
	OpWeakGt // for ^ operator in semver
	OpGte
	OpLt
	OpLte
)

func (op VersionOperator) String() string {
	switch op {
	case OpEq:
		return "equal"
	case OpWeakEq:
		return "weak equal"
	case OpNeq:
		return "not equal"
	case OpGt:
		return "greater than"
	case OpWeakGt:
		return "weak greater than"
	case OpGte:
		return "greater than or equal"
	case OpLt:
		return "less than"
	case OpLte:
		return "less than or equal"
	default:
		return "unknown"
	}
}

func (op VersionOperator) ToSign() string {
	switch op {
	case OpEq:
		return "="
	case OpWeakEq:
		return "~"
	case OpNeq:
		return "!="
	case OpGt:
		return ">"
	case OpWeakGt:
		return "^"
	case OpGte:
		return ">="
	case OpLt:
		return "<"
	case OpLte:
		return "<="
	default:
		return "unknown"
	}
}

func (op VersionOperator) Inverse() VersionOperator {
	switch op {
	case OpEq:
		return OpNeq
	case OpNeq:
		return OpEq
	case OpGt:
		return OpLte
	case OpWeakGt:
		return OpLte
	case OpGte:
		return OpLt
	case OpLt:
		return OpGte
	case OpLte:
		return OpGt
	default:
		return op
	}
}

func (op VersionOperator) Comparator() versionComparator {
	return operatorFunctions[op]
}
