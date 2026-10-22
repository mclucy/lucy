package catalog

import "github.com/mclucy/lucy/version"

// Name is a package name. User input may be untrusted; normalize it
// before using it as an identity.
type Name string

func (n Name) String() string {
	return string(n)
}

// Ref identifies one package supplied by a provider.
type Ref struct {
	Name     Name     `json:"name" yaml:"name"`
	Provider Provider `json:"provider,omitempty" yaml:"provider,omitempty"`
}

func (p Ref) String() string {
	return p.StringBase()
}

func (p Ref) StringBase() string {
	if p.Provider == ProviderUnspecified {
		return p.Name.String()
	}
	return p.Provider.String() + ":" + p.Name.String()
}

// VersionedRef identifies a package selection for one ecosystem.
// Version may be a selector during resolution or an exact version after
// resolution.
type VersionedRef struct {
	Ref       `yaml:",inline"`
	Ecosystem Ecosystem       `json:"ecosystem" yaml:"ecosystem"`
	Version   version.Version `json:"version" yaml:"version"`
}

func (p VersionedRef) String() string {
	suffix := ""
	if p.Version != version.Any {
		suffix = "@" + p.Version.String()
	}
	return p.Ref.String() + suffix
}

// StringBase is the stable provider-qualified package identity used by graph
// and provenance keys.
func (p VersionedRef) StringBase() string {
	return p.Ref.StringBase()
}

// StringQualified is the human-facing label that also carries the ecosystem.
func (p VersionedRef) StringQualified() string {
	return p.Ecosystem.String() + "/" + p.StringBase() + "@" + p.Version.String()
}
