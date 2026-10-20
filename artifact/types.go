// Package artifact provides types and interfaces for extracting metadata from
// package artifact files (JAR, ZIP, PYZ, MCDR plugin archives).
package artifact

import (
	"github.com/mclucy/lucy/types"
)

// Dependency represents a platform-qualified dependency detected from an
// artifact file. Artifact metadata does not establish upstream provenance.
type Dependency struct {
	Ref        types.VersionedPackageRef
	Constraint types.VersionExpr
	Mandatory  bool
	Origin     types.DependencyOrigin
}

type ArtifactCompatibility struct {
	FoliaSupported bool
}

// PackageInfo represents metadata extracted from a single artifact file
// (JAR/ZIP/PYZ/MCDR). Ref carries both the observed ecosystem and version.
type PackageInfo struct {
	Ref           types.VersionedPackageRef
	FilePath      string
	Dependencies  []Dependency
	Metadata      types.Metadata
	Compatibility ArtifactCompatibility
}

func dependencyOriginForEmbedded(embedded bool) types.DependencyOrigin {
	if embedded {
		return types.DependencyEmbedded
	}
	return types.DependencyRegular
}
