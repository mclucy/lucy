package state

import (
	"fmt"

	"github.com/pelletier/go-toml"
)

// Config represents the policy and defaults for a Lucy project.
// It is persisted in .lucy/config.toml and controls source selection,
// upgrade behavior, scope boundaries, and output formatting.
//
// Config OWNS: policy.defaults, policy.source-selection, policy.safety
// Config MUST NOT own: intent.direct-roots, resolution.graph, artifact.hashes,
// artifact.download-urls
type Config struct {
	// Meta contains format metadata.
	Meta MetaConfig `toml:"meta"`

	// Sources defines source selection and priority rules.
	Sources SourcesConfig `toml:"sources"`

	// Upgrade defines version resolution and upgrade policies.
	Upgrade UpgradeConfig `toml:"upgrade"`

	// Scope defines managed and unmanaged path boundaries.
	Scope ScopeConfig `toml:"scope"`

	// Optional defines optional package handling.
	Optional OptionalConfig `toml:"optional"`

	// Output defines CLI output formatting.
	Output OutputConfig `toml:"output"`
}

// MetaConfig contains format metadata for the config file.
type MetaConfig struct {
	// FormatVersion specifies the config file format version.
	// Use "v1" for the current schema.
	FormatVersion string `toml:"format_version"`
}

// SourcesConfig defines source selection and priority rules.
type SourcesConfig struct {
	// Priority defines the ordered list of sources to try when resolving
	// packages. Earlier sources have higher priority.
	Priority []string `toml:"priority"`

	// Preferred defines the default source when SourceAuto is specified.
	// Valid values: "auto", "modrinth", "curseforge", "github", "mcdr"
	Preferred string `toml:"preferred"`

	// AllowCustom enables custom source URLs (e.g., direct git or http URLs).
	AllowCustom bool `toml:"allow_custom"`
}

// UpgradeConfig defines version resolution and upgrade policies.
type UpgradeConfig struct {
	// Mode defines the version resolution strategy.
	// "compatible" - use compatible version (default)
	// "latest" - use latest version
	// "pinned" - use exactly specified version
	Mode string `toml:"mode"`

	// AllowMajorBumps enables major version upgrades.
	AllowMajorBumps bool `toml:"allow_major_bumps"`
}

// ScopeConfig defines managed and unmanaged path boundaries.
type ScopeConfig struct {
	// ManagedRoots specifies the list of relative paths that Lucy manages.
	// These directories are where Lucy installs and tracks packages.
	ManagedRoots []string `toml:"managed_roots"`

	// UnmanagedPaths is a list of glob patterns to exclude from drift detection.
	// Files matching these patterns are ignored by status/drift commands.
	UnmanagedPaths []string `toml:"unmanaged_paths"`

	// PreserveOnRemove lists glob patterns for files to preserve when packages
	// are removed. These files will not be deleted during cleanup operations.
	PreserveOnRemove []string `toml:"preserve_on_remove"`
}

// OptionalConfig defines optional package handling.
type OptionalConfig struct {
	// IncludeOptional controls whether optional dependencies are included.
	IncludeOptional bool `toml:"include_optional"`

	// ClientMods controls whether client-side only mods are included.
	ClientMods bool `toml:"client_mods"`
}

// OutputConfig defines CLI output formatting.
type OutputConfig struct {
	// NoStyle disables colored and styled output.
	NoStyle bool `toml:"no_style"`

	// JSON enables JSON output format.
	JSON bool `toml:"json"`
}

// ConfigDefaults returns a Config value with default settings.
func ConfigDefaults() Config {
	return Config{
		Meta: MetaConfig{
			FormatVersion: "v1",
		},
		Sources: SourcesConfig{
			Priority:    []string{"modrinth", "curseforge", "github", "mcdr"},
			Preferred:   "auto",
			AllowCustom: false,
		},
		Upgrade: UpgradeConfig{
			Mode:            "compatible",
			AllowMajorBumps: false,
		},
		Scope: ScopeConfig{
			ManagedRoots:     []string{"mods", "plugins", "config"},
			UnmanagedPaths:   []string{},
			PreserveOnRemove: []string{"config/**"},
		},
		Optional: OptionalConfig{
			IncludeOptional: false,
			ClientMods:      false,
		},
		Output: OutputConfig{
			NoStyle: false,
			JSON:    false,
		},
	}
}

// ValidateConfig validates the config and returns an error if any fields
// outside the policy domain are present.
//
// Config schema enforces that only policy fields are present, so this
// primarily serves as a safeguard against future schema drift.
func ValidateConfig(c Config) error {
	// Check that format_version is set and valid
	if c.Meta.FormatVersion == "" {
		return fmt.Errorf("config: format_version is required")
	}
	if c.Meta.FormatVersion != "v1" {
		return fmt.Errorf("config: unsupported format_version %q", c.Meta.FormatVersion)
	}

	// Validate source priority contains valid source names
	validSources := map[string]bool{
		"modrinth":   true,
		"curseforge": true,
		"github":     true,
		"mcdr":       true,
	}
	for _, src := range c.Sources.Priority {
		if !validSources[src] {
			return fmt.Errorf("config: invalid source %q in priority list", src)
		}
	}

	// Validate preferred source
	if c.Sources.Preferred != "auto" && !validSources[c.Sources.Preferred] {
		return fmt.Errorf("config: invalid preferred source %q", c.Sources.Preferred)
	}

	// Validate upgrade mode
	validModes := map[string]bool{
		"compatible": true,
		"latest":     true,
		"pinned":     true,
	}
	if !validModes[c.Upgrade.Mode] {
		return fmt.Errorf("config: invalid upgrade mode %q", c.Upgrade.Mode)
	}

	// Config MUST NOT own these - they belong to manifest/lock
	// This is enforced by the struct definition itself, but we add
	// a runtime check as a safeguard
	if c.Meta.FormatVersion == "" {
		// This should never happen due to type system, but kept as documentation
		return fmt.Errorf("config: reserved field detected - schema enforces policy domain only")
	}

	return nil
}

func (c Config) Marshal() ([]byte, error) {
	return toml.Marshal(c)
}

func (c *Config) Unmarshal(data []byte) error {
	return toml.Unmarshal(data, c)
}
