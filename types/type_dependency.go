package types

import (
	"github.com/mclucy/lucy/internal/fn"
)

// Version is a package version. It may hold a selector during resolution or
// an exact version after resolution.
type Version string

func (v Version) String() string {
	switch v {
	case VersionAny, "":
		return "any"
	case VersionNone:
		return "none"
	case VersionUnknown:
		return "unknown"
	case VersionBeta:
		return "beta"
	case VersionStable:
		return "stable"
	}
	return string(v)
}

func (v Version) CanInfer() bool {
	switch v {
	case VersionAny, VersionBeta, VersionStable:
		return true
	}
	return false
}

func (v Version) IsInvalid() bool {
	switch v {
	case VersionNone, VersionUnknown:
		return true
	}
	return false
}

var (
	VersionAny     Version = "any"
	VersionBeta    Version = "beta"
	VersionStable  Version = "stable"
	VersionNone    Version = "none"
	VersionUnknown Version = "unknown"
)

// ResolvableVersion is an interface for comparable parsed versions.
//
// A nil ResolvableVersion represents an invalid or unparseable version.
//
// In principle, you cannot compare two versions with different schemes.
// Implementations should return false for cross-scheme comparisons.
type ResolvableVersion interface {
	Compare(v2 ResolvableVersion) (int, bool)
	Validate() bool
	Scheme() VersionScheme
}

type VersionScheme uint8

const (
	VersionSchemeUnknown VersionScheme = iota
	VersionSchemeSemver
	VersionSchemeMaven

	// MinecraftSnapshot docs:
	// https://zh.minecraft.wiki/w/%E7%89%88%E6%9C%AC%E6%A0%BC%E5%BC%8F#%E5%BF%AB%E7%85%A7%EF%BC%88Snapshot%EF%BC%89
	// https://www.minecraft.net/en-us/article/minecraft-new-version-numbering-system
	VersionSchemeMinecraftSnapshot
	VersionSchemeMinecraftRelease
)

// DependencyOrigin identifies how a dependency is satisfied.
type DependencyOrigin string

const (
	DependencyRegular        DependencyOrigin = "regular"
	DependencyServerProvided DependencyOrigin = "server-provided"
	DependencyEmbedded       DependencyOrigin = "embedded"
)

func NormalizeDependencyOrigin(origin DependencyOrigin) DependencyOrigin {
	if origin == "" {
		return DependencyRegular
	}
	return origin
}

func (d Dependency) IsServerProvided() bool {
	return NormalizeDependencyOrigin(d.Origin) == DependencyServerProvided
}

func (d Dependency) IsEmbedded() bool {
	return NormalizeDependencyOrigin(d.Origin) == DependencyEmbedded
}

// Dependency is a package dependency request. Constraint is a 2D array whose
// outer array is OR and inner array is AND; nil or empty means no constraint.
// ID.Version is always empty because Constraint carries the version selector.
//
// DependencyOrigin records how the dependency is satisfied. Regular
// dependencies resolve through upstream providers, server-provided
// dependencies come from the server environment, and embedded dependencies
// are physically bundled inside the parent artifact.
type Dependency struct {
	ID         VersionedPackageRef
	Constraint VersionExpr
	Mandatory  bool
	Origin     DependencyOrigin
}

type VersionExpr [][]VersionSubExpr

type VersionSubExpr struct {
	Value    ResolvableVersion
	Operator VersionOperator
}

// Inverse inverts the version constraint expression in-place.
func (exps VersionExpr) Inverse() VersionExpr {
	for i := range exps {
		for j := range exps[i] {
			exps[i][j].Inverse()
		}
	}
	return exps
}

// Inverse inverts the version constraint in-place.
func (exp *VersionSubExpr) Inverse() {
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

func (d Dependency) Satisfy(id VersionedPackageRef, v ResolvableVersion) bool {
	if id.Ecosystem != d.ID.Ecosystem || id.Name != d.ID.Name {
		return false
	}

	if d.Constraint == nil || fn.IsEmptyVector(d.Constraint) {
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

type VersionComparator func(p1, p2 ResolvableVersion) bool

type semverTuple interface {
	Major() uint64
	Minor() uint64
	Patch() uint64
}

func compareByOperator(op VersionOperator, p1, p2 ResolvableVersion) bool {
	if p1 == nil || p2 == nil {
		return false
	}
	cmp, ok := p1.Compare(p2)
	if !ok {
		reverseCmp, reverseOK := p2.Compare(p1)
		if !reverseOK {
			return false
		}
		cmp = -reverseCmp
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

func compareSemverWeakEq(p1, p2 ResolvableVersion) bool {
	if p1 == nil || p2 == nil {
		return false
	}
	if p1.Scheme() != VersionSchemeSemver || p2.Scheme() != VersionSchemeSemver {
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

func compareSemverWeakGt(p1, p2 ResolvableVersion) bool {
	if p1 == nil || p2 == nil {
		return false
	}
	if p1.Scheme() != VersionSchemeSemver || p2.Scheme() != VersionSchemeSemver {
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
	OpEq:     func(p1, p2 ResolvableVersion) bool { return compareByOperator(OpEq, p1, p2) },
	OpWeakEq: compareSemverWeakEq,
	OpNeq:    func(p1, p2 ResolvableVersion) bool { return compareByOperator(OpNeq, p1, p2) },
	OpGt:     func(p1, p2 ResolvableVersion) bool { return compareByOperator(OpGt, p1, p2) },
	OpWeakGt: compareSemverWeakGt,
	OpGte:    func(p1, p2 ResolvableVersion) bool { return compareByOperator(OpGte, p1, p2) },
	OpLt:     func(p1, p2 ResolvableVersion) bool { return compareByOperator(OpLt, p1, p2) },
	OpLte:    func(p1, p2 ResolvableVersion) bool { return compareByOperator(OpLte, p1, p2) },
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
