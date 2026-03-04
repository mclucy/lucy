package cache

import (
	"fmt"
	"time"
)

type PolicyConfig struct {
	MaxSize int64         `json:"max_size"`
	TTL     time.Duration `json:"ttl"`
}

type Policy struct {
	Metadata PolicyConfig `json:"metadata"`
	Artifact PolicyConfig `json:"artifact"`
}

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
