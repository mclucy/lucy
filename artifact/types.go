// Package artifact provides types and interfaces for extracting metadata from
// package artifact files (JAR, ZIP, PYZ, MCDR plugin archives).
//
// This package defines the data structures that artifact readers produce and the
// option types that configure reader behavior. It has no runtime logic — readers
// are implemented in separate files within this package.
package artifact

import (
	"context"

	"github.com/mclucy/lucy/types"
)

// SlugResolver normalizes a package name for a given platform.
// Injected via WithSlugResolver option. Nil means no resolution.
type SlugResolver func(
	ctx context.Context,
	platform types.Ecosystem,
	name types.BarePackageName,
) (types.BarePackageName, error)

// Dependency represents a platform-qualified dependency detected from an
// artifact file. Artifact metadata does not establish upstream provenance.
type Dependency struct {
	Ref        types.VersionedPackageRef
	Constraint types.VersionExpr
	Mandatory  bool
	Type       types.DependencyType
}

type ArtifactCompatibility struct {
	FoliaSupported bool
}

// Info represents metadata extracted from a single artifact file
// (JAR/ZIP/PYZ/MCDR). Its coordinate records an observed platform.
type Info struct {
	Ref           types.VersionedPackageRef
	Version       types.BareVersion
	FilePath      string
	Dependencies  []Dependency
	Metadata      types.Metadata
	Compatibility ArtifactCompatibility
}

func dependencyTypeForEmbedded(embedded bool) types.DependencyType {
	if embedded {
		return types.Embedded
	}
	return types.Regular
}
