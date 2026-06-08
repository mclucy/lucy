package state

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
)

// GlobalConfigPath returns the user-level Lucy config path.
func GlobalConfigPath() string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		configDir = "."
	}
	return filepath.Join(configDir, "lucy", "config.yaml")
}

// ReadGlobalConfig reads the user-level config if it exists.
func ReadGlobalConfig() (*Config, bool, error) {
	path := GlobalConfigPath()
	data, ok, err := SafeRead(path)
	if err != nil || !ok {
		return nil, ok, err
	}
	cfg, err := ParseConfig(data)
	if err != nil {
		return nil, false, err
	}
	return cfg, true, nil
}

// WriteGlobalConfig writes the user-level config atomically.
func WriteGlobalConfig(c *Config) error {
	path := GlobalConfigPath()
	data, err := SerializeConfig(c)
	if err != nil {
		return err
	}
	if err := EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	if err := AtomicWrite(path, data, 0o600); err != nil {
		return fmt.Errorf("write global config: %w", err)
	}
	return nil
}

// MergeConfig applies non-zero workspace config fields over global config.
func MergeConfig(global, workspace *Config) *Config {
	if global == nil && workspace == nil {
		return nil
	}
	if global == nil {
		merged := *workspace
		return &merged
	}
	merged := *global
	if workspace == nil {
		return &merged
	}

	mergeNonZeroFields(reflect.ValueOf(&merged).Elem(), reflect.ValueOf(*workspace))
	return &merged
}

func mergeNonZeroFields(dst, src reflect.Value) {
	for i := 0; i < src.NumField(); i++ {
		srcField := src.Field(i)
		dstField := dst.Field(i)
		if srcField.Kind() == reflect.Struct {
			mergeNonZeroFields(dstField, srcField)
			continue
		}
		if !srcField.IsZero() {
			dstField.Set(srcField)
		}
	}
}
