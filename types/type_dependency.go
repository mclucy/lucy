package types

import (
	"github.com/mclucy/lucy/tools"
)

// RawVersion is the version of a package. Here we expect mods and plugins
// use semver (which they should). A known exception is Minecraft snapshots.
//
// There are several special constants for ambiguous(adaptive) versions.
// You MUST call upstream.InferVersion() before parsing them to ComparableVersion.
type RawVersion string

func (v RawVersion) String() string {
	switch v {
	case VersionAny, "":
		return "any"
	case VersionNone:
		return "none"
	case VersionUnknown:
		return "unknown"
	case VersionLatest:
		return "latest"
	case VersionCompatible:
		return "compatible"
	}
	return string(v)
}

func (v RawVersion) CanInfer() bool {
	switch v {
	case VersionAny, VersionLatest, VersionCompatible:
		return true
	}
	return false
}

func (v RawVersion) IsInvalid() bool {
	switch v {
	case VersionNone, VersionUnknown:
		return true
	}
	return false
}

var (
	VersionAny        RawVersion = "all"
	VersionNone       RawVersion = "none"
	VersionUnknown    RawVersion = "unknown"
	VersionLatest     RawVersion = "latest"
	VersionCompatible RawVersion = "compatible"
)

// ComparableVersion is an interface for comparable parsed versions.
//
// A nil ComparableVersion represents an invalid or unparseable version.
//
// In principle, you cannot compare two versions with different schemes.
// Implementations should return false for cross-scheme comparisons.
type ComparableVersion interface {
	// Compare compares this version with v2.
	// It returns:
	//   - -1 if this version < v2
	//   -  0 if this version == v2
	//   -  1 if this version > v2
	// The second return value is false when the two versions are not comparable
	// (for example, cross-scheme comparisons).
	Compare(v2 ComparableVersion) (int, bool)

	// Validate returns whether this version has valid, non-zero components.
	Validate() bool

	// Scheme returns the versioning scheme of this version.
	Scheme() VersionScheme
}

type VersionScheme uint8

const (
	Semver VersionScheme = iota

	// MinecraftSnapshot docs:
	// https://zh.minecraft.wiki/w/%E7%89%88%E6%9C%AC%E6%A0%BC%E5%BC%8F#%E5%BF%AB%E7%85%A7%EF%BC%88Snapshot%EF%BC%89
	// https://www.minecraft.net/en-us/article/minecraft-new-version-numbering-system
	MinecraftSnapshot
	MinecraftRelease
)

// Dependency represents a dependency requirement for a package.
//
// DO NOT read the Id.Version field. It is supposed to be empty.
//
// Dependency.Constraint is a 2D array. The outer array is OR and the inner
// array is AND. nil/empty means no constraint (all versions acceptable).
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

// Inverse inverts the version constraint expression in-place.
func (exps VersionConstraintExpression) Inverse() VersionConstraintExpression {
	for i := range exps {
		for j := range exps[i] {
			exps[i][j].Inverse()
		}
	}
	return exps
}

// Inverse inverts the version constraint in-place.
func (exp *VersionConstraint) Inverse() {
	switch exp.Operator {
	case OpEq:
		exp.Operator = OpNeq
	case OpNeq:
		exp.Operator = OpEq
	case OpGt, OpWeakGt:
		exp.Operator = OpLte
	case OpGte:
		exp.Operator = OpLt
	case OpLt:
		exp.Operator = OpGte
	case OpLte:
		exp.Operator = OpGt
	}
}

func (d Dependency) Satisfy(id PackageId, v ComparableVersion) bool {
	if (id.Platform != d.Id.Platform) || (id.Name != d.Id.Name) {
		return false
	}

	if d.Constraint == nil || tools.IsEmptyVector(d.Constraint) {
		return true
	}

	for _, orStatements := range d.Constraint {
		satisfied := true
		for _, andStatements := range orStatements {
			cmp := andStatements.Operator.Comparator()
			if v == nil || andStatements.Value == nil || !cmp(
				v,
				andStatements.Value,
			) {
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

type VersionComparator func(p1, p2 ComparableVersion) bool

type semverTuple interface {
	Major() uint64
	Minor() uint64
	Patch() uint64
}

func compareByOperator(op VersionOperator, p1, p2 ComparableVersion) bool {
	if p1 == nil || p2 == nil {
		return false
	}
	cmp, ok := p1.Compare(p2)
	if !ok {
		return false
	}
	switch op {
	case OpEq:
		return cmp == 0
	case OpNeq:
		return cmp != 0
	case OpGt:
		return cmp > 0
	case OpGte:
		return cmp >= 0
	case OpLt:
		return cmp < 0
	case OpLte:
		return cmp <= 0
	default:
		return false
	}
}

func compareSemverWeakEq(p1, p2 ComparableVersion) bool {
	if p1 == nil || p2 == nil {
		return false
	}
	if p1.Scheme() != Semver || p2.Scheme() != Semver {
		return false
	}
	candidate, ok1 := p1.(semverTuple)
	base, ok2 := p2.(semverTuple)
	if !ok1 || !ok2 {
		return false
	}
	if base.Minor() == 0 && base.Patch() == 0 {
		return candidate.Major() == base.Major()
	}
	return candidate.Major() == base.Major() && candidate.Minor() == base.Minor()
}

func compareSemverWeakGt(p1, p2 ComparableVersion) bool {
	if p1 == nil || p2 == nil {
		return false
	}
	if p1.Scheme() != Semver || p2.Scheme() != Semver {
		return false
	}
	candidate, ok1 := p1.(semverTuple)
	base, ok2 := p2.(semverTuple)
	if !ok1 || !ok2 {
		return false
	}
	if candidate.Major() != base.Major() {
		return false
	}
	return compareByOperator(OpGt, p1, p2)
}

var operatorFunctions = map[VersionOperator]VersionComparator{
	OpEq: func(p1, p2 ComparableVersion) bool {
		return compareByOperator(
			OpEq,
			p1,
			p2,
		)
	},
	OpWeakEq: compareSemverWeakEq,
	OpNeq: func(p1, p2 ComparableVersion) bool {
		return compareByOperator(
			OpNeq,
			p1,
			p2,
		)
	},
	OpGt: func(p1, p2 ComparableVersion) bool {
		return compareByOperator(
			OpGt,
			p1,
			p2,
		)
	},
	OpWeakGt: compareSemverWeakGt,
	OpGte: func(p1, p2 ComparableVersion) bool {
		return compareByOperator(
			OpGte,
			p1,
			p2,
		)
	},
	OpLt: func(p1, p2 ComparableVersion) bool {
		return compareByOperator(
			OpLt,
			p1,
			p2,
		)
	},
	OpLte: func(p1, p2 ComparableVersion) bool {
		return compareByOperator(
			OpLte,
			p1,
			p2,
		)
	},
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
	case OpGt, OpWeakGt:
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

func (op VersionOperator) Comparator() VersionComparator {
	return operatorFunctions[op]
}
