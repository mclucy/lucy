package types

// BarePackageName is an untrusted package name. Usually from user input. It might
// be invalid.
type BarePackageName string

type PackageRef struct {
	Eco  Ecosystem
	Name BarePackageName
}

func (p PackageRef) StringFull() string {
	return p.StringBase()
}

func (p PackageRef) StringBase() string {
	return p.Eco.String() + "/" + p.Name.String()
}

type VersionedPackageRef struct {
	PackageRef
	Version BareVersion
}

type ScopedPackageRef struct {
	PackageRef
	Scope SourceId
}

func (p ScopedPackageRef) StringBase() string {
	return p.PackageRef.StringBase()
}

func (p ScopedPackageRef) StringFull() string {
	return p.Scope.String() + ":" + p.PackageRef.StringFull()
}

type FullPackageRef struct {
	PackageRef
	Version BareVersion
	Scope   SourceId
}

func (p FullPackageRef) StringBase() string {
	return p.PackageRef.StringBase()
}

func (p FullPackageRef) StringFull() string {
	return p.Scope.String() + ":" + p.PackageRef.StringFull()
}

type StringablePackageRef interface {
	StringFull() string
	StringBase() string
}
