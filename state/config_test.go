package state

import (
	"bytes"
	"testing"
)

func TestConfigRoundTrip(t *testing.T) {
	cfg := ConfigDefaults()

	// First marshal
	first, err := cfg.Marshal()
	if err != nil {
		t.Fatalf("first marshal failed: %v", err)
	}

	// Unmarshal back
	var parsed Config
	if err := parsed.Unmarshal(first); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	// Re-marshal
	second, err := parsed.Marshal()
	if err != nil {
		t.Fatalf("second marshal failed: %v", err)
	}

	// Compare bytes for stability
	if !bytes.Equal(first, second) {
		t.Errorf("round-trip produced different output:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestConfigDefaults(t *testing.T) {
	cfg := ConfigDefaults()

	if cfg.Meta.FormatVersion != "v1" {
		t.Errorf("expected format_version v1, got %q", cfg.Meta.FormatVersion)
	}

	if len(cfg.Sources.Priority) != 4 {
		t.Errorf("expected 4 priority sources, got %d", len(cfg.Sources.Priority))
	}
	if cfg.Sources.Priority[0] != "modrinth" {
		t.Errorf("expected first priority source modrinth, got %q", cfg.Sources.Priority[0])
	}
	if cfg.Sources.Preferred != "auto" {
		t.Errorf("expected preferred auto, got %q", cfg.Sources.Preferred)
	}
	if cfg.Sources.AllowCustom != false {
		t.Errorf("expected allow_custom false, got %v", cfg.Sources.AllowCustom)
	}

	if cfg.Upgrade.Mode != "compatible" {
		t.Errorf("expected upgrade mode compatible, got %q", cfg.Upgrade.Mode)
	}
	if cfg.Upgrade.AllowMajorBumps != false {
		t.Errorf("expected allow_major_bumps false, got %v", cfg.Upgrade.AllowMajorBumps)
	}

	if len(cfg.Scope.ManagedRoots) != 3 {
		t.Errorf("expected 3 managed roots, got %d", len(cfg.Scope.ManagedRoots))
	}
	if cfg.Scope.ManagedRoots[0] != "mods" {
		t.Errorf("expected first managed root mods, got %q", cfg.Scope.ManagedRoots[0])
	}
	if len(cfg.Scope.PreserveOnRemove) != 1 {
		t.Errorf("expected 1 preserve pattern, got %d", len(cfg.Scope.PreserveOnRemove))
	}

	if cfg.Optional.IncludeOptional != false {
		t.Errorf("expected include_optional false, got %v", cfg.Optional.IncludeOptional)
	}
	if cfg.Optional.ClientMods != false {
		t.Errorf("expected client_mods false, got %v", cfg.Optional.ClientMods)
	}

	if cfg.Output.NoStyle != false {
		t.Errorf("expected no_style false, got %v", cfg.Output.NoStyle)
	}
	if cfg.Output.JSON != false {
		t.Errorf("expected json false, got %v", cfg.Output.JSON)
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid default config",
			cfg:     ConfigDefaults(),
			wantErr: false,
		},
		{
			name: "missing format_version",
			cfg: Config{
				Meta: MetaConfig{FormatVersion: ""},
			},
			wantErr: true,
			errMsg:  "format_version is required",
		},
		{
			name: "invalid format_version",
			cfg: Config{
				Meta: MetaConfig{FormatVersion: "v2"},
			},
			wantErr: true,
			errMsg:  "unsupported format_version",
		},
		{
			name: "invalid source in priority",
			cfg: Config{
				Meta: MetaConfig{FormatVersion: "v1"},
				Sources: SourcesConfig{
					Priority: []string{"invalid"},
				},
			},
			wantErr: true,
			errMsg:  "invalid source",
		},
		{
			name: "invalid preferred source",
			cfg: Config{
				Meta: MetaConfig{FormatVersion: "v1"},
				Sources: SourcesConfig{
					Preferred: "invalid",
				},
			},
			wantErr: true,
			errMsg:  "invalid preferred source",
		},
		{
			name: "invalid upgrade mode",
			cfg: Config{
				Meta: MetaConfig{FormatVersion: "v1"},
				Upgrade: UpgradeConfig{
					Mode: "invalid",
				},
			},
			wantErr: true,
			errMsg:  "invalid upgrade mode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateConfig(tt.cfg)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestConfigSerializationDeterminism(t *testing.T) {
	cfg := ConfigDefaults()

	// Marshal multiple times
	var results [][]byte
	for i := 0; i < 5; i++ {
		b, err := cfg.Marshal()
		if err != nil {
			t.Fatalf("marshal %d failed: %v", i, err)
		}
		results = append(results, b)
	}

	// Compare all outputs
	for i := 1; i < len(results); i++ {
		if !bytes.Equal(results[0], results[i]) {
			t.Errorf("serialization not deterministic: marshal 0 and %d differ", i)
		}
	}
}
