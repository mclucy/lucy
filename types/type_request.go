package types

// PackageRequest is the install package boundary object. Callers construct it
// only after package input has been parsed into a concrete package ref and a
// source scope has been chosen.
type PackageRequest struct {
	FullPackageRef
}
