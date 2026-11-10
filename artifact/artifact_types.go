// Package artifact provides types and interfaces for extracting metadata from
// package artifact files (JAR, ZIP, PYZ, MCDR plugin archives).
package artifact

import (
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/version"
)

// Dependency represents a platform-qualified dependency detected from an
// artifact file. Artifact metadata does not establish upstream provenance.
type Dependency struct {
	Ref        catalog.VersionedRef
	Constraint version.Expr
	Mandatory  bool
	Kind       catalog.DependencyKind
	Origin     catalog.DependencyOrigin
	// AnyOf lists alternative identities that satisfy this dependency in
	// place of Ref. Ref and Constraint describe the first alternative.
	AnyOf []Dependency
	// AllOf lists further conditions that must all match for this dependency
	// to apply. Quilt breaks arrays are conjunctive.
	AllOf []Dependency
	// Unless lists conditions that exempt this dependency. Any matching
	// condition suppresses it.
	Unless []Dependency
}

type Compatibility struct {
	FoliaSupported bool
}

// PackageInfo represents metadata extracted from a single artifact file
// (JAR/ZIP/PYZ/MCDR). Ref carries both the observed ecosystem and version.
type PackageInfo struct {
	Ref           catalog.VersionedRef
	FilePath      string
	Dependencies  []Dependency
	Metadata      catalog.Metadata
	Compatibility Compatibility
}

func dependencyOriginForEmbedded(embedded bool) catalog.DependencyOrigin {
	if embedded {
		return catalog.DependencyEmbedded
	}
	return catalog.DependencyRegular
}
