package state

import (
	"testing"
)

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
