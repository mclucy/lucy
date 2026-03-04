package cache

import (
	"fmt"
	"time"
)

type CacheConfig struct {
	Enabled           bool          `json:"enabled"`
	DownloadMaxSize   int64         `json:"download_max_size"`
	DownloadKeepFor   time.Duration `json:"download_keep_for"`
	IndexMaxSize      int64         `json:"index_max_size"`
	IndexRefreshAfter time.Duration `json:"index_refresh_after"`
}

func DefaultCacheConfig() CacheConfig {
	return CacheConfig{
		Enabled:           true,
		DownloadMaxSize:   2 * 1024 * 1024 * 1024, // 2 GB
		DownloadKeepFor:   7 * 24 * time.Hour,     // 7 days
		IndexMaxSize:      50 * 1024 * 1024,       // 50 MB
		IndexRefreshAfter: 4 * time.Hour,          // 4 hours
	}
}

func (c *CacheConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	if c.DownloadMaxSize <= 0 {
		return fmt.Errorf("download_max_size must be positive, got %d", c.DownloadMaxSize)
	}
	if c.DownloadKeepFor <= 0 {
		return fmt.Errorf("download_keep_for must be positive, got %s", c.DownloadKeepFor)
	}
	if c.IndexMaxSize <= 0 {
		return fmt.Errorf("index_max_size must be positive, got %d", c.IndexMaxSize)
	}
	if c.IndexRefreshAfter <= 0 {
		return fmt.Errorf("index_refresh_after must be positive, got %s", c.IndexRefreshAfter)
	}
	return nil
}

func (c *CacheConfig) toPolicy() Policy {
	return Policy{
		Metadata: PolicyConfig{
			MaxSize: c.IndexMaxSize,
			TTL:     c.IndexRefreshAfter,
		},
		Artifact: PolicyConfig{
			MaxSize: c.DownloadMaxSize,
			TTL:     c.DownloadKeepFor,
		},
	}
}
