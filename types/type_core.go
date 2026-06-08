package types

// PackageName is the slug of the package, using hyphens as separators. For example,
// "fabric-api".
//
// It is non-case-sensitive, though lowercase is recommended. Underlines '_' are
// equivalent to hyphens.
//
// A slug from an upstream API is preferred, if possible. Otherwise, the slug is
// obtained from the executable file. No exceptions since a package must either
// exist on a remote API or user's local files.
type PackageName string

type PackageRef struct {
	Platform Platform
	Name     PackageName
}

// PackageRequest is the universal desired state descriptor.
// This includes:
//   - via `lucy add`
//   - resolving from manifest
//
// This is data only. Ownership roles such as required/transitive/ignored, and
// relation roles such as dependency/embedded, are supplied by the surrounding
// context rather than stored here.
type PackageRequest struct {
	Ref      PackageRef
	Version  BareVersion
	Optional bool
	Source   Source
}
