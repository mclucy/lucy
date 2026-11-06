package upstream

import (
	"errors"

	"github.com/mclucy/lucy/catalog"
)

// ErrDependencyMetadataUnavailable reports that a provider could not obtain
// the build's dependency declarations. It is not evidence that the build
// declares no dependencies; consumers must treat it as unknown.
var ErrDependencyMetadataUnavailable = errors.New(
	"dependency metadata unavailable",
)

// DependencySet carries dependency metadata and records whether the
// dependencies were read from the artifact itself.
//
// An empty Dependencies slice asserts that the build declares no dependencies.
// A provider that cannot obtain dependency metadata must return
// ErrDependencyMetadataUnavailable instead of an empty set, so callers can tell
// "none declared" apart from "not reported".
type DependencySet struct {
	Dependencies []catalog.Dependency
	FromArtifact bool
}

// ResolvedPackage is an upstream package coordinate with download metadata.
type ResolvedPackage struct {
	ID            catalog.VersionedRef
	FileURL       string
	Filename      string
	Hash          string
	HashAlgorithm string
}
