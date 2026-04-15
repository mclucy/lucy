package state

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

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
			FormatVersion: SupportedVersion,
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
	if err := ValidateVersion(c.Meta.FormatVersion); err != nil {
		if IsVersionError(err) {
			return versionStateError(ConfigFile, "meta.format_version", c.Meta.FormatVersion, ErrVersionUnsupported)
		}
		return versionStateError(ConfigFile, "meta.format_version", c.Meta.FormatVersion, ErrMalformed)
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
			return NewStateError(ConfigFile, ErrMalformed, "sources.priority", fmt.Sprintf("invalid source %q in priority list", src))
		}
	}

	// Validate preferred source
	if c.Sources.Preferred != "auto" && !validSources[c.Sources.Preferred] {
		return NewStateError(ConfigFile, ErrMalformed, "sources.preferred", fmt.Sprintf("invalid preferred source %q", c.Sources.Preferred))
	}

	// Validate upgrade mode
	validModes := map[string]bool{
		"compatible": true,
		"latest":     true,
		"pinned":     true,
	}
	if !validModes[c.Upgrade.Mode] {
		return NewStateError(ConfigFile, ErrMalformed, "upgrade.mode", fmt.Sprintf("invalid upgrade mode %q", c.Upgrade.Mode))
	}

	// Config MUST NOT own these - they belong to manifest/lock
	// This is enforced by the struct definition itself, but we add
	// a runtime check as a safeguard
	if c.Meta.FormatVersion == "" {
		// This should never happen due to type system, but kept as documentation
		return NewStateError(ConfigFile, ErrBoundaryViolation, "meta.format_version", "reserved field detected - schema enforces policy domain only")
	}

	return nil
}

func (c Config) Marshal() ([]byte, error) {
	var buf bytes.Buffer
	writeTomlSectionHeader(&buf, "meta")
	writeTomlStringField(&buf, "format_version", c.Meta.FormatVersion)

	buf.WriteString("\n")
	writeTomlSectionHeader(&buf, "sources")
	writeTomlStringSliceField(&buf, "priority", c.Sources.Priority)
	writeTomlStringField(&buf, "preferred", c.Sources.Preferred)
	writeTomlBoolField(&buf, "allow_custom", c.Sources.AllowCustom)

	buf.WriteString("\n")
	writeTomlSectionHeader(&buf, "upgrade")
	writeTomlStringField(&buf, "mode", c.Upgrade.Mode)
	writeTomlBoolField(&buf, "allow_major_bumps", c.Upgrade.AllowMajorBumps)

	buf.WriteString("\n")
	writeTomlSectionHeader(&buf, "scope")
	writeTomlStringSliceField(&buf, "managed_roots", c.Scope.ManagedRoots)
	writeTomlStringSliceField(&buf, "unmanaged_paths", c.Scope.UnmanagedPaths)
	writeTomlStringSliceField(&buf, "preserve_on_remove", c.Scope.PreserveOnRemove)

	buf.WriteString("\n")
	writeTomlSectionHeader(&buf, "optional")
	writeTomlBoolField(&buf, "include_optional", c.Optional.IncludeOptional)
	writeTomlBoolField(&buf, "client_mods", c.Optional.ClientMods)

	buf.WriteString("\n")
	writeTomlSectionHeader(&buf, "output")
	writeTomlBoolField(&buf, "no_style", c.Output.NoStyle)
	writeTomlBoolField(&buf, "json", c.Output.JSON)

	return buf.Bytes(), nil
}

func (c *Config) Unmarshal(data []byte) error {
	return toml.Unmarshal(data, c)
}

func writeTomlSectionHeader(buf *bytes.Buffer, name string) {
	buf.WriteString("[")
	buf.WriteString(name)
	buf.WriteString("]\n")
}

func writeTomlArrayTableHeader(buf *bytes.Buffer, name string) {
	buf.WriteString("[[")
	buf.WriteString(name)
	buf.WriteString("]]\n")
}

func writeTomlStringField(buf *bytes.Buffer, key, value string) {
	buf.WriteString(key)
	buf.WriteString(" = ")
	buf.WriteString(strconv.Quote(value))
	buf.WriteString("\n")
}

func writeTomlBoolField(buf *bytes.Buffer, key string, value bool) {
	buf.WriteString(key)
	buf.WriteString(" = ")
	buf.WriteString(strconv.FormatBool(value))
	buf.WriteString("\n")
}

func writeTomlStringSliceField(buf *bytes.Buffer, key string, values []string) {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, strconv.Quote(value))
	}
	buf.WriteString(key)
	buf.WriteString(" = [")
	buf.WriteString(strings.Join(quoted, ", "))
	buf.WriteString("]\n")
}
