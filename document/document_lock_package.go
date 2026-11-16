package document

import (
	"fmt"
	"path/filepath"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/upstream"
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

// PackageDependency is a relationship the selected release declares to
// another project.
type PackageDependency struct {
	Provider  catalog.Provider       `yaml:"provider" json:"provider"`
	ProjectID string                 `yaml:"project_id" json:"project_id"`
	ReleaseID string                 `yaml:"release_id,omitempty" json:"release_id,omitempty"`
	Kind      catalog.DependencyKind `yaml:"kind" json:"kind"`
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
	// Dependencies retains the release's declared project relationships, so a
	// candidate restored from the lock keeps its demands and incompatibilities.
	Dependencies []PackageDependency `yaml:"dependencies,omitempty" json:"dependencies,omitempty"`
	// DependenciesUnknown records that the provider could not supply the
	// release's dependency metadata.
	DependenciesUnknown bool `yaml:"dependencies_unknown,omitempty" json:"dependencies_unknown,omitempty"`
	// Channel records the selected release's stability channel, which the
	// artifact does not carry. A candidate restored from the lock must satisfy
	// the same selector checks as the catalogue candidate it replaces.
	Channel upstream.ReleaseChannel `yaml:"channel,omitempty" json:"channel,omitempty"`
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

// PackageDirectory derives placement even for disabled or absent packages.
// MCDR packages require a wrapper runtime in the lock.
func (l *Lock) PackageDirectory(pkg *Package) (string, error) {
	directory := l.Server.Directory
	folder := "mods"
	if pkg.Environment == server.EnvironmentMCDR {
		if l.MCDR == nil {
			return "", fmt.Errorf("MCDR package has no wrapper runtime")
		}
		directory, folder = l.MCDR.Directory, "plugins"
	} else if pkg.Ecosystem == catalog.EcosystemBukkit ||
		pkg.Ecosystem == catalog.EcosystemPaper {
		folder = "plugins"
	}
	return filepath.Join(directory, folder), nil
}

// PackageDestination identifies a planned path, not filesystem existence or
// ownership. Nested filenames remain relative to the package directory.
func (l *Lock) PackageDestination(pkg *Package) (string, error) {
	directory, err := l.PackageDirectory(pkg)
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, pkg.Filename), nil
}
