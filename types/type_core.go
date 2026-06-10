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

type StringablePackageRef interface {
	StringFull() string
	StringBase() string
}
