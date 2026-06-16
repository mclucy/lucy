package types

// BarePackageName is an untrusted package name. Usually from user input. It might
// be invalid.
type BarePackageName string

type PackageRef struct {
	Platform PlatformId
	Name     BarePackageName
}

func (p PackageRef) IsIdentityPackage() bool {
	_, exists := platformByIdentityPackage[p.Name]
	return exists
}

func (p PackageRef) StringFull() string {
	return p.StringBase()
}

func (p PackageRef) StringBase() string {
	return p.Platform.String() + "/" + p.Name.String()
}

type VersionedPackageRef struct {
	PackageRef
	Version BareVersion
}

type ScopedPackageRef struct {
	PackageRef
	Scope SourceId
}

type StringablePackageRef interface {
	StringFull() string
	StringBase() string
}
