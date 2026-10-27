package document

import (
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/version"
)

// RequirementBinding retains a declared reference's immutable project identity,
// including when the requirement is disabled.
type RequirementBinding struct {
	Reference   string          `yaml:"reference" json:"reference"`
	Selector    version.Version `yaml:"selector" json:"selector"`
	FilePattern string          `yaml:"file_pattern,omitempty" json:"file_pattern,omitempty"`
	Enabled     bool            `yaml:"enabled" json:"enabled"`
}

// Package records an artifact pin and its requirement bindings, not installed state.
type Package struct {
	Environment      server.Environment `yaml:"environment" json:"environment"`
	Ecosystem        catalog.Ecosystem  `yaml:"ecosystem" json:"ecosystem"`
	catalog.Artifact `yaml:",inline"`
	Requirements     []RequirementBinding `yaml:"requirements,omitempty" json:"requirements,omitempty"`
	// Dependency records selection through another package's dependency.
	Dependency bool `yaml:"dependency,omitempty" json:"dependency,omitzero"`
	// DependencyOf keeps the required dependents that justify this pin.
	DependencyOf []catalog.Ref `yaml:"dependency_of,omitempty" json:"dependency_of,omitempty"`
}

// Enabled reports whether p is selected for installation. Dependency demand
// overrides disabled direct bindings; an unbound package without dependents is ambient.
func (p *Package) Enabled() bool {
	if p.Dependency || (len(p.Requirements) == 0 && len(p.DependencyOf) == 0) {
		return true
	}
	for _, r := range p.Requirements {
		if r.Enabled {
			return true
		}
	}
	return false
}
