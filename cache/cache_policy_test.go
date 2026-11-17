package cache

import (
	"testing"
	"time"
)

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
