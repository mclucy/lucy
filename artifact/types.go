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
	Origin     catalog.DependencyOrigin
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
