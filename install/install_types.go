package install

import (
	"github.com/mclucy/lucy/types"
)

// PackageRequest is the universal desired state descriptor, i.e., the main
// entrance of this package.
// This includes:
//   - via `lucy add`
//   - resolving from manifest
//
// A scope is enforced to ensure that the package is unambiguously
// identifiable. An installation cannot be requested without a scope
type PackageRequest struct {
	types.ScopedPackageRef
	Version types.BareVersion
}

type InstallOptions struct {
	WithOptional bool
	Force        bool
}

func DefaultOptions() InstallOptions {
	return InstallOptions{}
}
