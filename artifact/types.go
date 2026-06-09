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
	platform types.PlatformId,
	name types.BarePackageName,
) (types.BarePackageName, error)

// ArtifactDep represents a dependency detected from an artifact file.
type ArtifactDep struct {
	Ref        types.PackageRef
	Constraint types.VersionExpr
	Mandatory  bool
	Embedded   bool
}

// ArtifactInfo represents metadata extracted from a single artifact file
// (JAR/ZIP/PYZ/MCDR).
type ArtifactInfo struct {
	Ref          types.PackageRef
	Version      types.BareVersion
	FilePath     string
	Dependencies []ArtifactDep
	Metadata     types.Metadata
	Supports     *types.PlatformSupport
}
