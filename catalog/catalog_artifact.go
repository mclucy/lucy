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
	ID             string                 `yaml:"id" json:"id"`
	Version        version.Version        `yaml:"version" json:"version"`
	Ecosystem      Ecosystem              `yaml:"ecosystem" json:"ecosystem"`
	VersionDialect version.Dialect        `yaml:"version_dialect,omitempty" json:"version_dialect,omitempty"`
	NestedPath     string                 `yaml:"nested_path,omitempty" json:"nested_path,omitempty"`
	DescriptorPath string                 `yaml:"descriptor_path,omitempty" json:"descriptor_path,omitempty"`
	Provides       []string               `yaml:"provides,omitempty" json:"provides,omitempty"`
	Side           Side                   `yaml:"side,omitempty" json:"side,omitempty"`
	FoliaSupported bool                   `yaml:"folia_supported,omitempty" json:"folia_supported,omitzero"`
	APIVersion     string                 `yaml:"api_version,omitempty" json:"api_version,omitempty"`
	Dependencies   []DescriptorDependency `yaml:"dependencies,omitempty" json:"dependencies,omitempty"`
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
}
