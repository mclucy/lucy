package cache

import (
	"testing"
	"time"
)

func TestCanonicalizeKeyNormalizesEquivalentURLs(t *testing.T) {
	t.Parallel()
	equivalents := []string{
		"HTTPS://Example.COM:443/a/../b?z=1&a=2#fragment",
		"https://example.com/b?a=2&z=1",
		"https://example.com/b?z=1&a=2",
		"https://example.com/./b?z=1&a=2#other",
	}
	want := canonicalizeKey(equivalents[0])
	for _, value := range equivalents[1:] {
		if got := canonicalizeKey(value); got != want {
			t.Fatalf("canonicalizeKey(%q) = %q, want %q", value, got, want)
		}
	}
	if want != "https://example.com/b?a=2&z=1" {
		t.Fatalf("canonical key = %q", want)
	}
	if again := canonicalizeKey(string(want)); again != want {
		t.Fatalf("canonical key is not idempotent: %q -> %q", want, again)
	}
}

func TestCanonicalizeKeyLeavesOpaqueKeysAlone(t *testing.T) {
	t.Parallel()
	tests := []string{
		"key-shared-a",
		"",
		"http://[::1",
		"sha256:not-a-url",
	}
	for _, value := range tests {
		if got := canonicalizeKey(value); got != key(value) {
			t.Fatalf("canonicalizeKey(%q) = %q, want the input", value, got)
		}
	}
}

func TestExpiredEntriesHonorsExpirationBoundary(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	entries := map[key]*Entry{
		"before": {Expiration: now.Add(-time.Second)},
		"at":     {Expiration: now},
		"after":  {Expiration: now.Add(time.Second)},
	}
	expired := expiredEntries(entries, now)
	if len(expired) != 2 {
		t.Fatalf("expired entries = %v, want the before and at boundaries", expired)
	}
	for _, k := range expired {
		if k == "after" {
			t.Fatal("expiredEntries kept a live entry and dropped the boundary case")
		}
	}
}

func TestEvictionCandidatesEvictsOldestWithinBudget(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	policy := Policy{
		Artifact: PolicyConfig{MaxSize: 10, TTL: time.Hour},
	}
	entries := map[key]*Entry{
		"oldest": {Kind: KindArtifact, Size: 6, ContentHash: "hash-oldest", Expiration: now.Add(-2 * time.Hour)},
		"newest": {Kind: KindArtifact, Size: 6, ContentHash: "hash-newest", Expiration: now.Add(-time.Hour)},
	}
	targets := evictionCandidates(entries, policy)
	if len(targets) != 1 || targets[0].key != "oldest" {
		t.Fatalf("eviction targets = %+v, want only the oldest entry", targets)
	}
}

func TestEvictionCandidatesCountsSharedContentOnce(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	policy := Policy{
		Artifact: PolicyConfig{MaxSize: 6, TTL: time.Hour},
	}
	entries := map[key]*Entry{
		"shared-a": {Kind: KindArtifact, Size: 6, ContentHash: "shared", Expiration: now.Add(-3 * time.Hour)},
		"shared-b": {Kind: KindArtifact, Size: 6, ContentHash: "shared", Expiration: now.Add(-2 * time.Hour)},
		"unique":   {Kind: KindArtifact, Size: 6, ContentHash: "unique", Expiration: now.Add(-time.Hour)},
	}
	targets := evictionCandidates(entries, policy)
	if len(targets) != 2 || targets[0].key != "shared-a" || targets[1].key != "shared-b" {
		t.Fatalf("eviction targets = %+v, want both aliases of the shared blob", targets)
	}
}

func TestEvictionCandidatesOrdersTiesByKey(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	policy := Policy{
		Artifact: PolicyConfig{MaxSize: 5, TTL: time.Hour},
	}
	entries := map[key]*Entry{
		"b": {Kind: KindArtifact, Size: 4, ContentHash: "b", Expiration: now},
		"a": {Kind: KindArtifact, Size: 4, ContentHash: "a", Expiration: now},
	}
	targets := evictionCandidates(entries, policy)
	if len(targets) != 1 || targets[0].key != "a" {
		t.Fatalf("eviction targets = %+v, want the lexicographically first key", targets)
	}
}
