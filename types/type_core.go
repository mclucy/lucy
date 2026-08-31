package types

// BarePackageName is an untrusted package name. Usually from user input. It might
// be invalid.
type BarePackageName string

// PackageRef identifies an upstream package. SourceAuto is valid while routing
// an unresolved request; resolved upstream packages carry a concrete source.
type PackageRef struct {
	Name   BarePackageName
	Source SourceId
}

func (p PackageRef) StringFull() string {
	return p.StringBase()
}

func (p PackageRef) StringBase() string {
	return p.Source.String() + ":" + p.Name.String()
}

// VersionedPackageRef identifies a package selection for one ecosystem.
// Version may be a selector during resolution or an exact resolved version.
type VersionedPackageRef struct {
	PackageRef
	Eco     Ecosystem
	Version BareVersion
}

type StringablePackageRef interface {
	StringFull() string
	StringBase() string
}
