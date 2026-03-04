package cache

import (
	"fmt"
	"time"
)

// PolicyConfig defines cache behavior for a specific entry kind.
type PolicyConfig struct {
	MaxSize int64         `json:"max_size"`
	TTL     time.Duration `json:"ttl"`
}

// Policy holds separate configurations for metadata and artifact entries.
type Policy struct {
	Metadata PolicyConfig `json:"metadata"`
	Artifact PolicyConfig `json:"artifact"`
}

// Minecraft workload sizing rationale:
//
// Server JARs are ~50-80 MB each. Users typically keep 2-5 Minecraft versions
// available for testing or running multiple servers. Mod JARs range from 1-30 MB
// with a typical modpack pulling 50-200 mods. Metadata payloads (Mojang version
// manifests, Modrinth search results) are under 1 MB each.
const (
	// DefaultMetadataTTL controls how long version manifests and mod indexes
	// are considered fresh. 4 hours balances freshness against unnecessary
	// re-fetches — Mojang publishes snapshots roughly weekly and releases
	// less often.
	DefaultMetadataTTL = 4 * time.Hour

	// DefaultMetadataMaxSize caps total metadata cache at 50 MB. This
	// provides generous headroom given individual manifests are <1 MB.
	DefaultMetadataMaxSize int64 = 50 * 1024 * 1024

	// DefaultArtifactTTL controls how long downloaded binaries stay cached.
	// 7 days supports version switching without re-downloading, while
	// bounding disk usage for users who try many versions.
	DefaultArtifactTTL = 7 * 24 * time.Hour

	// DefaultArtifactMaxSize caps total artifact cache at 2 GB. This
	// supports caching ~5 server JARs (~80 MB each) plus a reasonable
	// mod collection (~200 mods at ~5 MB average).
	DefaultArtifactMaxSize int64 = 2 * 1024 * 1024 * 1024
)

// DefaultPolicy returns the default cache policy tuned for Minecraft
// server management workloads.
func DefaultPolicy() Policy {
	return Policy{
		Metadata: PolicyConfig{
			MaxSize: DefaultMetadataMaxSize,
			TTL:     DefaultMetadataTTL,
		},
		Artifact: PolicyConfig{
			MaxSize: DefaultArtifactMaxSize,
			TTL:     DefaultArtifactTTL,
		},
	}
}

// ConfigFor returns the PolicyConfig for the given entry kind.
func (p *Policy) ConfigFor(kind EntryKind) PolicyConfig {
	switch kind {
	case KindMetadata:
		return p.Metadata
	case KindArtifact:
		return p.Artifact
	default:
		return p.Artifact
	}
}

// Validate checks that all policy values are within acceptable bounds.
func (p *Policy) Validate() error {
	if err := p.Metadata.validate("metadata"); err != nil {
		return err
	}
	return p.Artifact.validate("artifact")
}

func (c *PolicyConfig) validate(name string) error {
	if c.MaxSize <= 0 {
		return fmt.Errorf(
			"invalid %s policy: max_size must be positive, got %d",
			name, c.MaxSize,
		)
	}
	if c.TTL <= 0 {
		return fmt.Errorf(
			"invalid %s policy: ttl must be positive, got %s",
			name, c.TTL,
		)
	}
	return nil
}
