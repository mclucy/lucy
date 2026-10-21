package types

import (
	"path"
	"time"
)

// Artifact records one upstream file. Descriptor module identities do not
// replace its provider coordinates.
type Artifact struct {
	Provider  Provider           `yaml:"provider" json:"provider"`
	ProjectID string             `yaml:"project_id,omitempty" json:"project_id,omitempty"`
	ReleaseID string             `yaml:"release_id,omitempty" json:"release_id,omitempty"`
	FileID    string             `yaml:"file_id,omitempty" json:"file_id,omitempty"`
	Version   Version            `yaml:"version" json:"version"`
	Embedded  string             `yaml:"embedded,omitempty" json:"embedded,omitempty"`
	Filename  string             `yaml:"filename" json:"filename"`
	URL       string             `yaml:"url,omitempty" json:"url,omitempty"`
	Hashes    map[string]string  `yaml:"hashes" json:"hashes"`
	Modules   []DescriptorModule `yaml:"modules,omitempty" json:"modules,omitempty"`
}

type DescriptorModule struct {
	ID             string                 `yaml:"id" json:"id"`
	Version        Version                `yaml:"version" json:"version"`
	Ecosystem      Ecosystem              `yaml:"ecosystem" json:"ecosystem"`
	VersionDialect VersionDialect         `yaml:"version_dialect,omitempty" json:"version_dialect,omitempty"`
	NestedPath     string                 `yaml:"nested_path,omitempty" json:"nested_path,omitempty"`
	DescriptorPath string                 `yaml:"descriptor_path,omitempty" json:"descriptor_path,omitempty"`
	Provides       []string               `yaml:"provides,omitempty" json:"provides,omitempty"`
	Side           Side                   `yaml:"side,omitempty" json:"side,omitempty"`
	FoliaSupported bool                   `yaml:"folia_supported,omitempty" json:"folia_supported,omitzero"`
	APIVersion     string                 `yaml:"api_version,omitempty" json:"api_version,omitempty"`
	Dependencies   []DescriptorDependency `yaml:"dependencies,omitempty" json:"dependencies,omitempty"`
}

// EffectiveVersionDialect returns the recorded dialect. Documents written
// before the field existed infer it from the descriptor format, then from the
// module ecosystem.
func (m DescriptorModule) EffectiveVersionDialect() VersionDialect {
	if m.VersionDialect != VersionDialectUnknown {
		return m.VersionDialect
	}
	switch path.Base(m.DescriptorPath) {
	case "fabric.mod.json":
		return VersionDialectFabric
	case "mods.toml", "neoforge.mods.toml", "sponge_plugins.json":
		return VersionDialectMaven
	case "mcdreforged.plugin.json":
		return VersionDialectMCDR
	case "plugin.yml", "paper-plugin.yml":
		return VersionDialectOpaque
	}
	switch m.Ecosystem {
	case EcosystemFabric:
		return VersionDialectFabric
	case EcosystemForge, EcosystemNeoForge, EcosystemSponge:
		return VersionDialectMaven
	case EcosystemMCDR:
		return VersionDialectMCDR
	case EcosystemMinecraft:
		return VersionDialectMinecraft
	case EcosystemBukkit, EcosystemPaper:
		return VersionDialectOpaque
	default:
		return VersionDialectUnknown
	}
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

// RequirementBinding retains a declared reference's immutable project identity,
// including when the requirement is disabled.
type RequirementBinding struct {
	Reference   string  `yaml:"reference" json:"reference"`
	Selector    Version `yaml:"selector" json:"selector"`
	FilePattern string  `yaml:"file_pattern,omitempty" json:"file_pattern,omitempty"`
	Enabled     bool    `yaml:"enabled" json:"enabled"`
}

type LockedPackage struct {
	Environment  Environment `yaml:"environment" json:"environment"`
	Ecosystem    Ecosystem   `yaml:"ecosystem" json:"ecosystem"`
	Artifact     `yaml:",inline"`
	Requirements []RequirementBinding `yaml:"requirements,omitempty" json:"requirements,omitempty"`
}

type BootstrapInput struct {
	Path     string `yaml:"path" json:"path"`
	Artifact `yaml:",inline"`
}

type ProjectDependency struct {
	Provider  Provider       `json:"provider"`
	ProjectID string         `json:"project_id"`
	ReleaseID string         `json:"release_id,omitempty"`
	Kind      DependencyKind `json:"kind"`
}

type CatalogueCandidate struct {
	Artifact
	Channel      ReleaseChannel
	PublishedAt  time.Time
	PrimaryFile  bool
	Dependencies []ProjectDependency
}

type ServerProductRequest struct {
	Minecraft      Version
	Product        ServerProduct
	ProductVersion Version
	Repository     string
	FilePattern    string
}

type ServerProductResolution struct {
	Minecraft      Version
	Product        ServerProduct
	ProductVersion Version
	Artifact       Artifact
	Bootstrapper   string
	Inputs         []BootstrapInput
	Java           int
}
