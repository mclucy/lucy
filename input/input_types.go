package input

import "github.com/mclucy/lucy/types"

// PackageRefResolver is an ambiguous/untrusted package ref from external input.
// It must be parsed to a canonical package name before further use.
type PackageRefResolver interface {
	ResolveLocal() types.ScopedPackageRef
	ResolveRemote() types.ScopedPackageRef
}
