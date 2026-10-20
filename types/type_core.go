package types

// PackageName is a package name. User input may be untrusted; normalize it
// before using it as an identity.
type PackageName string

func (n PackageName) String() string {
	return string(n)
}

// PackageRef identifies one package supplied by a provider.
type PackageRef struct {
	Name     PackageName `json:"name" yaml:"name"`
	Provider Provider    `json:"provider,omitempty" yaml:"provider,omitempty"`
}

func (p PackageRef) String() string {
	return p.StringBase()
}

func (p PackageRef) StringBase() string {
	if p.Provider == ProviderUnspecified {
		return p.Name.String()
	}
	return p.Provider.String() + ":" + p.Name.String()
}

// VersionedPackageRef identifies a package selection for one ecosystem.
// Version may be a selector during resolution or an exact version after
// resolution.
type VersionedPackageRef struct {
	PackageRef `yaml:",inline"`
	Ecosystem  Ecosystem `json:"ecosystem" yaml:"ecosystem"`
	Version    Version   `json:"version" yaml:"version"`
}

func (p VersionedPackageRef) String() string {
	version := ""
	if p.Version != VersionAny {
		version = "@" + p.Version.String()
	}
	return p.PackageRef.String() + version
}

// StringBase is the stable provider-qualified package identity used by graph
// and provenance keys.
func (p VersionedPackageRef) StringBase() string {
	return p.PackageRef.StringBase()
}

// StringQualified is the human-facing label that also carries the ecosystem.
func (p VersionedPackageRef) StringQualified() string {
	return p.Ecosystem.String() + "/" + p.StringBase() + "@" + p.Version.String()
}
