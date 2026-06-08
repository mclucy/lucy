package state

import (
	"bytes"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestConfigRoundTrip(t *testing.T) {
	cfg := ConfigDefaults()

	first, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("first marshal failed: %v", err)
	}

	var parsed Config
	if err := yaml.Unmarshal(first, &parsed); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	second, err := yaml.Marshal(parsed)
	if err != nil {
		t.Fatalf("second marshal failed: %v", err)
	}

	if !bytes.Equal(first, second) {
		t.Errorf("round-trip produced different output:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestConfigDefaults(t *testing.T) {
	cfg := ConfigDefaults()

	if len(cfg.Sources.Priority) != 4 {
		t.Errorf("expected 4 priority sources, got %d", len(cfg.Sources.Priority))
	}
	if cfg.Sources.Priority[0] != "modrinth" {
		t.Errorf("expected first priority source modrinth, got %q", cfg.Sources.Priority[0])
	}
	if cfg.Sources.Preferred != "auto" {
		t.Errorf("expected preferred auto, got %q", cfg.Sources.Preferred)
	}

	if cfg.Upgrade.Mode != "compatible" {
		t.Errorf("expected upgrade mode compatible, got %q", cfg.Upgrade.Mode)
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
			name: "invalid source in priority",
			cfg: Config{
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
			} else if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestConfigSerializationDeterminism(t *testing.T) {
	cfg := ConfigDefaults()

	var results [][]byte
	for i := range 5 {
		b, err := yaml.Marshal(cfg)
		if err != nil {
			t.Fatalf("marshal %d failed: %v", i, err)
		}
		results = append(results, b)
	}

	for i := 1; i < len(results); i++ {
		if !bytes.Equal(results[0], results[i]) {
			t.Errorf("serialization not deterministic: marshal 0 and %d differ", i)
		}
	}
}
