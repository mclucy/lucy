package install

import "github.com/mclucy/lucy/types"

// PackageRequest is the install package boundary object. Callers construct it
// only after package input has been parsed into a concrete package ref and a
// source scope has been chosen.
type PackageRequest struct {
	types.ScopedPackageRef
	Version types.BareVersion
}
