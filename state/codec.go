package state

import (
	"gopkg.in/yaml.v3"
)

// ParseConfig parses YAML config bytes into a validated Config.
func ParseConfig(data []byte) (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
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
	data, err := yaml.Marshal(c)
	if err != nil {
		return nil, malformedStateError(ConfigFile, "document", err)
	}
	return data, nil
}

// ParseManifest parses manifest bytes into a validated Manifest.
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
	data, err := m.Marshal()
	if err != nil {
		return nil, malformedStateError(ManifestFile, "document", err)
	}
	return data, nil
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

