package lockfile

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

// Package is one installed package recorded in the lockfile.
type Package struct {
	Environment      server.Environment `yaml:"environment" json:"environment"`
	Ecosystem        catalog.Ecosystem  `yaml:"ecosystem" json:"ecosystem"`
	catalog.Artifact `yaml:",inline"`
	Requirements     []RequirementBinding `yaml:"requirements,omitempty" json:"requirements,omitempty"`
}

// Enabled reports whether p belongs to the installed set: a package bound to no
// requirement is ambient, and a bound package needs one enabled requirement.
func (p *Package) Enabled() bool {
	if len(p.Requirements) == 0 {
		return true
	}
	for _, r := range p.Requirements {
		if r.Enabled {
			return true
		}
	}
	return false
}
