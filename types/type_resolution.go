package types

// Artifact records one upstream file. Native module identities do not replace
// its provider coordinates.
type Artifact struct {
	Provider  string            `yaml:"provider" json:"provider"`
	ProjectID string            `yaml:"project_id,omitempty" json:"project_id,omitempty"`
	ReleaseID string            `yaml:"release_id,omitempty" json:"release_id,omitempty"`
	FileID    string            `yaml:"file_id,omitempty" json:"file_id,omitempty"`
	Version   string            `yaml:"version" json:"version"`
	Embedded  string            `yaml:"embedded,omitempty" json:"embedded,omitempty"`
	Filename  string            `yaml:"filename" json:"filename"`
	URL       string            `yaml:"url,omitempty" json:"url,omitempty"`
	Hashes    map[string]string `yaml:"hashes" json:"hashes"`
	Modules   []NativeModule    `yaml:"modules,omitempty" json:"modules,omitempty"`
}

type NativeModule struct {
	ID             string             `yaml:"id" json:"id"`
	Version        string             `yaml:"version" json:"version"`
	Loader         Ecosystem          `yaml:"loader" json:"loader"`
	Member         string             `yaml:"member,omitempty" json:"member,omitempty"`
	Descriptor     string             `yaml:"descriptor,omitempty" json:"descriptor,omitempty"`
	Provides       []string           `yaml:"provides,omitempty" json:"provides,omitempty"`
	Environment    string             `yaml:"environment,omitempty" json:"environment,omitempty"`
	FoliaSupported bool               `yaml:"folia_supported,omitempty" json:"folia_supported,omitzero"`
	APIVersion     string             `yaml:"api_version,omitempty" json:"api_version,omitempty"`
	Dependencies   []NativeDependency `yaml:"dependencies,omitempty" json:"dependencies,omitempty"`
}

type NativeDependency struct {
	ID            string `yaml:"id" json:"id"`
	Constraint    string `yaml:"constraint,omitempty" json:"constraint,omitempty"`
	Kind          string `yaml:"kind" json:"kind"`
	Ordering      string `yaml:"ordering,omitempty" json:"ordering,omitempty"`
	Phase         string `yaml:"phase,omitempty" json:"phase,omitempty"`
	Load          string `yaml:"load,omitempty" json:"load,omitempty"`
	JoinClasspath bool   `yaml:"join_classpath,omitempty" json:"join_classpath,omitzero"`
	Provider      string `yaml:"provider,omitempty" json:"provider,omitempty"`
	ProjectID     string `yaml:"project_id,omitempty" json:"project_id,omitempty"`
	ModuleID      string `yaml:"module_id,omitempty" json:"module_id,omitempty"`
}

// RequirementBinding retains a declared reference's immutable project identity,
// including when the requirement is disabled.
type RequirementBinding struct {
	Reference string `yaml:"reference" json:"reference"`
	Selector  string `yaml:"selector" json:"selector"`
	File      string `yaml:"file,omitempty" json:"file,omitempty"`
	Enabled   bool   `yaml:"enabled" json:"enabled"`
}

type LockedPackage struct {
	Runtime      string    `yaml:"runtime" json:"runtime"`
	Loader       Ecosystem `yaml:"loader" json:"loader"`
	Artifact     `yaml:",inline"`
	Requirements []RequirementBinding `yaml:"requirements,omitempty" json:"requirements,omitempty"`
}

type BootstrapInput struct {
	Path     string `yaml:"path" json:"path"`
	Artifact `yaml:",inline"`
}

type ProjectDependency struct {
	Provider  string
	ProjectID string
	ReleaseID string
	Kind      string
}

type CatalogueCandidate struct {
	Artifact
	ReleaseType  string
	Published    string
	Primary      bool
	Dependencies []ProjectDependency
}

type CoreRequest struct {
	Minecraft    string
	Distribution string
	Version      string
	Repository   string
	File         string
}

type CoreResolution struct {
	Minecraft    string
	Distribution string
	Version      string
	Artifact     Artifact
	Bootstrap    string
	Inputs       []BootstrapInput
	Java         int
}
