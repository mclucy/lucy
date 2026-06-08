package state

import "fmt"

// Config represents the policy and defaults for a Lucy project.
// It is persisted in lucy.yaml as an optional config override section.
type Config struct {
	Sources SourcesConfig `yaml:"sources"`
	Upgrade UpgradeConfig `yaml:"upgrade"`
	Debug   DebugConfig   `yaml:"debug"`
}

// SourcesConfig defines source selection and priority rules.
type SourcesConfig struct {
	Priority  []string `yaml:"priority"`
	Preferred string   `yaml:"preferred"`
}

// UpgradeConfig defines version resolution and upgrade policies.
type UpgradeConfig struct {
	Mode string `yaml:"mode"`
}

// DebugConfig defines debug command settings.
type DebugConfig struct {
	// IdentityPackages lists package IDs (in "platform/name" format) that are
	// treated as server platform/loader packages rather than user mods. They are
	// excluded from the debug binary-search list.
	IdentityPackages []string `yaml:"identity_packages"`
}

// DebugConfigDefaults returns default debug settings.
func DebugConfigDefaults() DebugConfig {
	return DebugConfig{
		IdentityPackages: []string{
			"minecraft/minecraft",
			"minecraft/mc",
			"minecraft/vanilla",
			"fabric/fabric",
			"fabric/fabric-loader",
			"forge/forge",
			"neoforge/neoforge",
			"mcdr/mcdreforged",
			"mcdr/mcdr",
		},
	}
}

// ConfigDefaults returns a Config value with default settings.
func ConfigDefaults() Config {
	return Config{
		Sources: SourcesConfig{
			Priority:  []string{"modrinth", "curseforge", "github", "mcdr"},
			Preferred: "auto",
		},
		Upgrade: UpgradeConfig{
			Mode: "compatible",
		},
		Debug: DebugConfigDefaults(),
	}
}

// ValidateConfig validates the config fields.
func ValidateConfig(c Config) error {
	validSources := map[string]bool{
		"modrinth": true, "curseforge": true, "github": true, "mcdr": true,
	}
	for _, src := range c.Sources.Priority {
		if !validSources[src] {
			return NewStateError(ConfigFile, ErrMalformed, "sources.priority", fmt.Sprintf("invalid source %q in priority list", src))
		}
	}
	if c.Sources.Preferred != "auto" && !validSources[c.Sources.Preferred] {
		return NewStateError(ConfigFile, ErrMalformed, "sources.preferred", fmt.Sprintf("invalid preferred source %q", c.Sources.Preferred))
	}
	validModes := map[string]bool{
		"compatible": true, "latest": true, "pinned": true,
	}
	if c.Upgrade.Mode != "" && !validModes[c.Upgrade.Mode] {
		return NewStateError(ConfigFile, ErrMalformed, "upgrade.mode", fmt.Sprintf("invalid upgrade mode %q", c.Upgrade.Mode))
	}
	return nil
}

