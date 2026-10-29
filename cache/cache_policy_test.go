package cache

import (
	"strings"
	"testing"
	"time"
)

func TestPolicyValidate(t *testing.T) {
	t.Parallel()
	valid := Policy{
		Metadata: PolicyConfig{MaxSize: 1, TTL: time.Minute},
		Artifact: PolicyConfig{MaxSize: 2, TTL: time.Hour},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid policy rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*Policy)
		field  string
	}{
		{
			name:   "metadata size",
			mutate: func(p *Policy) { p.Metadata.MaxSize = 0 },
			field:  "metadata",
		},
		{
			name:   "metadata ttl",
			mutate: func(p *Policy) { p.Metadata.TTL = 0 },
			field:  "metadata",
		},
		{
			name:   "artifact size",
			mutate: func(p *Policy) { p.Artifact.MaxSize = -1 },
			field:  "artifact",
		},
		{
			name:   "artifact ttl",
			mutate: func(p *Policy) { p.Artifact.TTL = -time.Second },
			field:  "artifact",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			policy := valid
			test.mutate(&policy)
			err := policy.Validate()
			if err == nil {
				t.Fatalf("policy %+v was accepted", policy)
			}
			if !strings.Contains(err.Error(), test.field) {
				t.Fatalf("error %q does not name %s", err, test.field)
			}
		})
	}
}

func TestPolicyConfigFor(t *testing.T) {
	t.Parallel()
	policy := Policy{
		Metadata: PolicyConfig{MaxSize: 1, TTL: time.Minute},
		Artifact: PolicyConfig{MaxSize: 2, TTL: time.Hour},
	}
	if got := policy.ConfigFor(KindMetadata); got != policy.Metadata {
		t.Fatalf("metadata config = %+v, want %+v", got, policy.Metadata)
	}
	if got := policy.ConfigFor(KindArtifact); got != policy.Artifact {
		t.Fatalf("artifact config = %+v, want %+v", got, policy.Artifact)
	}
	if got := policy.ConfigFor(EntryKind(255)); got != policy.Artifact {
		t.Fatalf("unknown kind config = %+v, want the artifact default", got)
	}
}
