package catalog

import "github.com/mclucy/lucy/version"

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
	ID         VersionedRef
	Constraint version.Expr
	Mandatory  bool
	Origin     DependencyOrigin
}
