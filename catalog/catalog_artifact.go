package catalog

import "github.com/mclucy/lucy/version"

// Artifact records one upstream file. Descriptor module identities do not
// replace its provider coordinates.
type Artifact struct {
	Provider  Provider           `yaml:"provider" json:"provider"`
	ProjectID string             `yaml:"project_id,omitempty" json:"project_id,omitempty"`
	ReleaseID string             `yaml:"release_id,omitempty" json:"release_id,omitempty"`
	FileID    string             `yaml:"file_id,omitempty" json:"file_id,omitempty"`
	Version   version.Version    `yaml:"version" json:"version"`
	Embedded  string             `yaml:"embedded,omitempty" json:"embedded,omitempty"`
	Filename  string             `yaml:"filename" json:"filename"`
	URL       string             `yaml:"url,omitempty" json:"url,omitempty"`
	Hashes    map[string]string  `yaml:"hashes" json:"hashes"`
	Modules   []DescriptorModule `yaml:"modules,omitempty" json:"modules,omitempty"`
}

type DescriptorModule struct {
	ID             string          `yaml:"id" json:"id"`
	Version        version.Version `yaml:"version" json:"version"`
	Ecosystem      Ecosystem       `yaml:"ecosystem" json:"ecosystem"`
	VersionDialect version.Dialect `yaml:"version_dialect,omitempty" json:"version_dialect,omitempty"`
	NestedPath     string          `yaml:"nested_path,omitempty" json:"nested_path,omitempty"`
	DescriptorPath string          `yaml:"descriptor_path,omitempty" json:"descriptor_path,omitempty"`
	// Group is the namespace the descriptor assigns to the module identity.
	// Quilt descriptors require it and use it to qualify the mod id.
	Group    string   `yaml:"group,omitempty" json:"group,omitempty"`
	Provides []string `yaml:"provides,omitempty" json:"provides,omitempty"`
	// ProvidedVersions records the version each provided identity exposes when
	// it differs from this module's version. Providers that omit an entry
	// expose Version, matching the descriptor default.
	ProvidedVersions map[string]version.Version `yaml:"provided_versions,omitempty" json:"provided_versions,omitempty"`
	// ClientOnly lists module identities this artifact declares for the
	// client environment only. Loaders do not enforce a dependency whose only
	// candidate is excluded from the dedicated server, so the resolver ignores
	// such a requirement instead of reporting it missing.
	ClientOnly     []string               `yaml:"client_only,omitempty" json:"client_only,omitempty"`
	Side           Side                   `yaml:"side,omitempty" json:"side,omitempty"`
	FoliaSupported bool                   `yaml:"folia_supported,omitempty" json:"folia_supported,omitzero"`
	APIVersion     string                 `yaml:"api_version,omitempty" json:"api_version,omitempty"`
	Dependencies   []DescriptorDependency `yaml:"dependencies,omitempty" json:"dependencies,omitempty"`
}

// ProvidedVersion returns the version this module exposes for the provided
// identity id. A provided identity without an explicit version exposes the
// module's own version.
func (m DescriptorModule) ProvidedVersion(id string) version.Version {
	if provided, ok := m.ProvidedVersions[id]; ok && provided != "" {
		return provided
	}
	return m.Version
}

type DescriptorDependency struct {
	ID            string         `yaml:"id" json:"id"`
	Constraint    string         `yaml:"constraint,omitempty" json:"constraint,omitempty"`
	Kind          DependencyKind `yaml:"kind" json:"kind"`
	Ordering      string         `yaml:"ordering,omitempty" json:"ordering,omitempty"`
	Phase         string         `yaml:"phase,omitempty" json:"phase,omitempty"`
	Load          string         `yaml:"load,omitempty" json:"load,omitempty"`
	JoinClasspath bool           `yaml:"join_classpath,omitempty" json:"join_classpath,omitzero"`
	Provider      Provider       `yaml:"provider,omitempty" json:"provider,omitempty"`
	ProjectID     string         `yaml:"project_id,omitempty" json:"project_id,omitempty"`
	ModuleID      string         `yaml:"module_id,omitempty" json:"module_id,omitempty"`
	// AnyOf lists alternative identities that satisfy this dependency in
	// place of ID. ID and Constraint describe the first alternative, and the
	// alternatives are satisfied by any one match. Quilt dependency arrays
	// produce them when their entries name different modules.
	AnyOf []DescriptorDependency `yaml:"any_of,omitempty" json:"any_of,omitempty"`
	// AllOf lists further conditions that must all match for this dependency
	// to apply. Quilt breaks arrays are conjunctive: a break takes effect only
	// when every listed dependency matches.
	AllOf []DescriptorDependency `yaml:"all_of,omitempty" json:"all_of,omitempty"`
	// Unless lists conditions that exempt this dependency. Any matching
	// condition suppresses the dependency entirely. Empty means the
	// dependency always applies.
	Unless []DescriptorDependency `yaml:"unless,omitempty" json:"unless,omitempty"`
}
