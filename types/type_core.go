package types

// BarePackageName is an untrusted package name. Usually from user input. It might
// be invalid.
type BarePackageName string

type PackageRef struct {
	Platform PlatformId
	Name     BarePackageName
}

type VersionedPackageRef struct {
	Platform PlatformId
	Name     BarePackageName
	Version  BareVersion
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
	Source   SourceId
}
