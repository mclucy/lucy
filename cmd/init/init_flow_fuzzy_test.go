package init

import (
	"strings"
	"testing"

	"github.com/mclucy/lucy/state"
)

func TestBuildResultKeepsFuzzyManifestIntentAndExactLockSkeleton(t *testing.T) {
	s := NewInitFlowState(t.TempDir())
	s.GameVersion = "1.21.4"
	s.Platform = "fabric"
	s.PlatformVersion = "0.16.10"
	s.ManagedRoots = []string{"mods"}
	s.PackageClassifications = []TakeoverPackageClassification{{
		ID:      "fabric/lithium",
		Version: ">=0.12.0 <0.13.0",
		Source:  "modrinth",
		Role:    state.RoleRequired,
		Leaf:    true,
	}}

	result, err := BuildResult(s)
	if err != nil {
		t.Fatalf("build result failed: %v", err)
	}
	if result.ManifestToWrite == nil || result.LockToWrite == nil {
		t.Fatal("expected init to produce both manifest and lock outputs")
	}

	if got := result.ManifestToWrite.Packages[0].Version; got != ">=0.12.0 <0.13.0" {
		t.Fatalf("expected init manifest to preserve fuzzy intent, got %q", got)
	}
	if len(result.LockToWrite.Packages) != 0 {
		t.Fatalf("expected init lock skeleton to remain exact-and-empty until resolution, got %#v", result.LockToWrite.Packages)
	}
	if !strings.HasPrefix(result.LockToWrite.ManifestFingerprint, "sha256:") {
		t.Fatalf("expected init lock fingerprint to be set, got %q", result.LockToWrite.ManifestFingerprint)
	}
	if err := state.ValidateLock(*result.LockToWrite); err != nil {
		t.Fatalf("expected init lock skeleton to validate: %v", err)
	}
}
