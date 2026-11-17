package cache

import "time"

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
