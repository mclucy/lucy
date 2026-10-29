package cache

import (
	"strings"
	"testing"
	"time"
)

func TestConfigValidate(t *testing.T) {
	t.Parallel()
	valid := DefaultCacheConfig()
	if err := valid.Validate(); err != nil {
		t.Fatalf("default config rejected: %v", err)
	}

	disabled := Config{}
	if err := disabled.Validate(); err != nil {
		t.Fatalf("disabled config rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*Config)
		field  string
	}{
		{
			name:   "zero download size",
			mutate: func(c *Config) { c.DownloadMaxSize = 0 },
			field:  "download_max_size",
		},
		{
			name:   "negative download size",
			mutate: func(c *Config) { c.DownloadMaxSize = -1 },
			field:  "download_max_size",
		},
		{
			name:   "zero retention",
			mutate: func(c *Config) { c.DownloadKeepFor = 0 },
			field:  "download_keep_for",
		},
		{
			name:   "negative retention",
			mutate: func(c *Config) { c.DownloadKeepFor = -time.Second },
			field:  "download_keep_for",
		},
		{
			name:   "zero index size",
			mutate: func(c *Config) { c.IndexMaxSize = 0 },
			field:  "index_max_size",
		},
		{
			name:   "negative index size",
			mutate: func(c *Config) { c.IndexMaxSize = -1 },
			field:  "index_max_size",
		},
		{
			name:   "zero refresh",
			mutate: func(c *Config) { c.IndexRefreshAfter = 0 },
			field:  "index_refresh_after",
		},
		{
			name:   "negative refresh",
			mutate: func(c *Config) { c.IndexRefreshAfter = -time.Second },
			field:  "index_refresh_after",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config := valid
			test.mutate(&config)
			err := config.Validate()
			if err == nil {
				t.Fatalf("config %+v was accepted", config)
			}
			if !strings.Contains(err.Error(), test.field) {
				t.Fatalf("error %q does not name %s", err, test.field)
			}
		})
	}
}

func TestConfigToPolicy(t *testing.T) {
	t.Parallel()
	config := Config{
		Enabled:           true,
		DownloadMaxSize:   11,
		DownloadKeepFor:   time.Minute,
		IndexMaxSize:      22,
		IndexRefreshAfter: time.Hour,
	}
	policy := config.toPolicy()
	if policy.Metadata.MaxSize != config.IndexMaxSize ||
		policy.Metadata.TTL != config.IndexRefreshAfter {
		t.Fatalf("metadata policy = %+v, want index limits", policy.Metadata)
	}
	if policy.Artifact.MaxSize != config.DownloadMaxSize ||
		policy.Artifact.TTL != config.DownloadKeepFor {
		t.Fatalf("artifact policy = %+v, want download limits", policy.Artifact)
	}
}
