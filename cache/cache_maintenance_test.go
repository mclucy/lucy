package cache

import (
	"testing"
	"time"
)

func TestMaintainLockedRejectsMalformedContentHash(t *testing.T) {
	t.Parallel()
	client := installNetworkCache(t, DefaultCacheConfig())
	handler := client.cache()
	handler.index.put("broken", &Entry{
		Kind:        KindArtifact,
		ContentHash: "sha256:not-a-digest",
		Expiration:  time.Now().Add(-time.Hour),
	})
	if _, err := handler.maintainLocked(nil); err == nil {
		t.Fatal("maintenance accepted a malformed content hash")
	}
	if _, ok := handler.index.get("broken"); !ok {
		t.Fatal("maintenance removed an entry it could not resolve")
	}
}

func TestCollectAllLockedRejectsMalformedContentHash(t *testing.T) {
	t.Parallel()
	client := installNetworkCache(t, DefaultCacheConfig())
	handler := client.cache()
	handler.index.put("broken", &Entry{
		Kind:        KindArtifact,
		ContentHash: "sha256:not-a-digest",
	})
	var report ResetReport
	if err := handler.collectAllLocked(&report); err == nil {
		t.Fatal("object collection accepted a malformed content hash")
	}
}
