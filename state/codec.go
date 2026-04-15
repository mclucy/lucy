package state

import (
	"bytes"
	"strconv"
)

// ParseConfig parses TOML config bytes into a validated Config.
func ParseConfig(data []byte) (*Config, error) {
	var cfg Config
	if err := cfg.Unmarshal(data); err != nil {
		return nil, malformedStateError(ConfigFile, "document", err)
	}
	if err := ValidateConfig(cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// SerializeConfig serializes a validated Config deterministically.
func SerializeConfig(c *Config) ([]byte, error) {
	if c == nil {
		return nil, NewStateError(ConfigFile, ErrMalformed, "document", "config is nil")
	}
	if err := ValidateConfig(*c); err != nil {
		return nil, err
	}
	return marshalConfigDeterministic(c), nil
}

// ParseManifest parses TOML manifest bytes into a validated Manifest.
func ParseManifest(data []byte) (*Manifest, error) {
	var manifest Manifest
	if err := manifest.Unmarshal(data); err != nil {
		return nil, malformedStateError(ManifestFile, "document", err)
	}
	if err := ValidateManifest(manifest); err != nil {
		return nil, err
	}
	return &manifest, nil
}

// SerializeManifest serializes a validated Manifest deterministically.
func SerializeManifest(m *Manifest) ([]byte, error) {
	if m == nil {
		return nil, NewStateError(ManifestFile, ErrMalformed, "document", "manifest is nil")
	}
	if err := ValidateManifest(*m); err != nil {
		return nil, err
	}
	return marshalManifestDeterministic(m), nil
}

// ParseLock parses JSON lock bytes into a validated Lock.
func ParseLock(data []byte) (*Lock, error) {
	var lock Lock
	if err := lock.Unmarshal(data); err != nil {
		return nil, malformedStateError(LockFile, "document", err)
	}
	if err := ValidateLock(lock); err != nil {
		return nil, err
	}
	return &lock, nil
}

// SerializeLock serializes a validated Lock deterministically.
func SerializeLock(l *Lock) ([]byte, error) {
	if l == nil {
		return nil, NewStateError(LockFile, ErrMalformed, "document", "lock is nil")
	}
	if err := ValidateLock(*l); err != nil {
		return nil, err
	}
	data, err := l.Marshal()
	if err != nil {
		return nil, malformedStateError(LockFile, "document", err)
	}
	return data, nil
}

func marshalConfigDeterministic(c *Config) []byte {
	var buf bytes.Buffer

	buf.WriteString("[meta]\n")
	buf.WriteString("format_version = ")
	buf.WriteString(strconv.Quote(c.Meta.FormatVersion))
	buf.WriteString("\n\n")

	buf.WriteString("[sources]\n")
	buf.WriteString("priority = ")
	buf.WriteString(formatStringArray(c.Sources.Priority))
	buf.WriteString("\n")
	buf.WriteString("preferred = ")
	buf.WriteString(strconv.Quote(c.Sources.Preferred))
	buf.WriteString("\n")
	buf.WriteString("allow_custom = ")
	buf.WriteString(strconv.FormatBool(c.Sources.AllowCustom))
	buf.WriteString("\n\n")

	buf.WriteString("[upgrade]\n")
	buf.WriteString("mode = ")
	buf.WriteString(strconv.Quote(c.Upgrade.Mode))
	buf.WriteString("\n")
	buf.WriteString("allow_major_bumps = ")
	buf.WriteString(strconv.FormatBool(c.Upgrade.AllowMajorBumps))
	buf.WriteString("\n\n")

	buf.WriteString("[scope]\n")
	buf.WriteString("managed_roots = ")
	buf.WriteString(formatStringArray(c.Scope.ManagedRoots))
	buf.WriteString("\n")
	buf.WriteString("unmanaged_paths = ")
	buf.WriteString(formatStringArray(c.Scope.UnmanagedPaths))
	buf.WriteString("\n")
	buf.WriteString("preserve_on_remove = ")
	buf.WriteString(formatStringArray(c.Scope.PreserveOnRemove))
	buf.WriteString("\n\n")

	buf.WriteString("[optional]\n")
	buf.WriteString("include_optional = ")
	buf.WriteString(strconv.FormatBool(c.Optional.IncludeOptional))
	buf.WriteString("\n")
	buf.WriteString("client_mods = ")
	buf.WriteString(strconv.FormatBool(c.Optional.ClientMods))
	buf.WriteString("\n\n")

	buf.WriteString("[output]\n")
	buf.WriteString("no_style = ")
	buf.WriteString(strconv.FormatBool(c.Output.NoStyle))
	buf.WriteString("\n")
	buf.WriteString("json = ")
	buf.WriteString(strconv.FormatBool(c.Output.JSON))
	buf.WriteString("\n")

	return buf.Bytes()
}

func marshalManifestDeterministic(m *Manifest) []byte {
	var buf bytes.Buffer

	buf.WriteString("[format]\n")
	buf.WriteString("version = ")
	buf.WriteString(strconv.Quote(m.Format.Version))
	buf.WriteString("\n\n")

	buf.WriteString("[environment]\n")
	buf.WriteString("game_version = ")
	buf.WriteString(strconv.Quote(m.Environment.GameVersion))
	buf.WriteString("\n")
	buf.WriteString("platform = ")
	buf.WriteString(strconv.Quote(m.Environment.Platform))
	buf.WriteString("\n")
	buf.WriteString("platform_version = ")
	buf.WriteString(strconv.Quote(m.Environment.PlatformVersion))
	buf.WriteString("\n")
	buf.WriteString("compatible_platforms = ")
	buf.WriteString(formatStringArray(m.Environment.CompatiblePlatforms))
	buf.WriteString("\n\n")

	buf.WriteString("[sources]\n")
	for _, custom := range m.Sources.Custom {
		buf.WriteString("[[sources.custom]]\n")
		buf.WriteString("name = ")
		buf.WriteString(strconv.Quote(custom.Name))
		buf.WriteString("\n")
		buf.WriteString("url = ")
		buf.WriteString(strconv.Quote(custom.URL))
		buf.WriteString("\n")
		buf.WriteString("type = ")
		buf.WriteString(strconv.Quote(custom.Type))
		buf.WriteString("\n")
	}
	buf.WriteString("\n")

	buf.WriteString("[layout]\n")
	buf.WriteString("mods_dir = ")
	buf.WriteString(strconv.Quote(m.Layout.ModsDir))
	buf.WriteString("\n")
	buf.WriteString("plugins_dir = ")
	buf.WriteString(strconv.Quote(m.Layout.PluginsDir))
	buf.WriteString("\n")
	buf.WriteString("config_dir = ")
	buf.WriteString(strconv.Quote(m.Layout.ConfigDir))
	buf.WriteString("\n\n")

	buf.WriteString("[policy]\n")
	buf.WriteString("managed_roots = ")
	buf.WriteString(formatStringArray(m.Policy.ManagedRoots))
	buf.WriteString("\n")
	buf.WriteString("unmanaged_paths = ")
	buf.WriteString(formatStringArray(m.Policy.UnmanagedPaths))
	buf.WriteString("\n")

	for _, pkg := range m.Packages {
		buf.WriteString("\n[[packages]]\n")
		buf.WriteString("id = ")
		buf.WriteString(strconv.Quote(pkg.ID))
		buf.WriteString("\n")
		buf.WriteString("version = ")
		buf.WriteString(strconv.Quote(pkg.Version))
		buf.WriteString("\n")
		buf.WriteString("source = ")
		buf.WriteString(strconv.Quote(pkg.Source))
		buf.WriteString("\n")
		buf.WriteString("role = ")
		buf.WriteString(strconv.Quote(string(pkg.Role)))
		buf.WriteString("\n")
		buf.WriteString("side = ")
		buf.WriteString(strconv.Quote(string(pkg.Side)))
		buf.WriteString("\n")
		buf.WriteString("optional = ")
		buf.WriteString(strconv.FormatBool(pkg.Optional))
		buf.WriteString("\n")
		buf.WriteString("pinned = ")
		buf.WriteString(strconv.FormatBool(pkg.Pinned))
		buf.WriteString("\n")
	}

	for _, bundle := range m.Bundles {
		buf.WriteString("\n[[bundles]]\n")
		buf.WriteString("name = ")
		buf.WriteString(strconv.Quote(bundle.Name))
		buf.WriteString("\n")
		buf.WriteString("type = ")
		buf.WriteString(strconv.Quote(string(bundle.Type)))
		buf.WriteString("\n")
		buf.WriteString("path = ")
		buf.WriteString(strconv.Quote(bundle.Path))
		buf.WriteString("\n")
		buf.WriteString("source = ")
		buf.WriteString(strconv.Quote(bundle.Source))
		buf.WriteString("\n")
		buf.WriteString("optional = ")
		buf.WriteString(strconv.FormatBool(bundle.Optional))
		buf.WriteString("\n")
	}

	return buf.Bytes()
}

func formatStringArray(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, strconv.Quote(value))
	}
	return "[" + joinQuoted(quoted) + "]"
}

func joinQuoted(values []string) string {
	if len(values) == 0 {
		return ""
	}
	var buf bytes.Buffer
	for i, value := range values {
		if i > 0 {
			buf.WriteString(", ")
		}
		buf.WriteString(value)
	}
	return buf.String()
}
