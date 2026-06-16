package init

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/mclucy/lucy/state"
	"github.com/mclucy/lucy/types"
)

// Regression tests for takeover, MCDR, fuzzy versions, multi-platform,
// and mixed manual/managed scenarios.

func TestTakeoverWithPartialExistingLucyState_RespectsObservationPrecedence(t *testing.T) {
	tmpDir := t.TempDir()

	// Create partial Lucy state: lucy.yaml exists but lucy-lock.yaml is missing
	// This simulates a server that was previously partially initialized.
	cfg := state.ConfigDefaults()
	if err := state.WriteConfig(tmpDir, &cfg); err != nil {
		t.Fatalf("write config: %v", err)
	}
	// Create the mods dir to satisfy managed root existence not strictly required but realistic.
	if err := os.MkdirAll(filepath.Join(tmpDir, "mods"), 0o755); err != nil {
		t.Fatalf("mkdir mods: %v", err)
	}

	// Run init on this partial state with --conflict=preserve (default behavior).
	s := NewInitFlowState(tmpDir)
	s.GameVersion = "1.21.4"
	s.Platform = "fabric"
	s.PlatformVersion = "0.16.10"
	s.ConflictResolution = PreserveExisting

	result, err := BuildResult(s)
	if err != nil {
		t.Fatalf("build result: %v", err)
	}

	// ConfigToWrite should be nil because preserve mode keeps existing.
	if result.ConfigToWrite != nil {
		t.Error("expected ConfigToWrite to be nil in preserve mode when config exists")
	}
	if !slices.Contains(result.SkippedFiles, string(state.ConfigFile)) {
		t.Errorf("expected config to be skipped, got %v", result.SkippedFiles)
	}

	// Manifest is merged into lucy.yaml, so preserve mode keeps it with config.
	if result.ManifestToWrite != nil {
		t.Error("expected manifest to be preserved with existing lucy.yaml")
	}
	// Lock should still be scaffolded since it doesn't exist.
	if result.LockToWrite == nil {
		t.Error("expected lock skeleton to be scaffolded")
	}
}

func TestTakeoverWithInconsistentExistingState_TracksConflicts(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := state.ConfigDefaults()
	if err := state.WriteConfig(tmpDir, &cfg); err != nil {
		t.Fatalf("write config: %v", err)
	}
	malformedManifest := []byte(`format_version: v1
environment:
  game_version: 1.21.4
  modding_platform: invalid-platform
packages: []
bundles: []
`)
	if err := os.WriteFile(
		filepath.Join(tmpDir, string(state.ManifestFile)),
		malformedManifest,
		0o644,
	); err != nil {
		t.Fatalf("write malformed manifest: %v", err)
	}

	s := NewInitFlowState(tmpDir)
	if len(s.ExistingStateConflicts) == 0 {
		t.Error("expected existing state conflict to be tracked")
	}

	s.GameVersion = "1.21.4"
	s.Platform = "none"

	_, err := BuildResult(s)
	if err == nil {
		t.Fatal("expected build to fail when existing state conflicts exist")
	}
}

func TestMultiPlatformSelection_NeoforgePlusFabricPlusMCDR(t *testing.T) {
	s := &InitFlowState{
		GameVersion:         "1.21.4",
		Platform:            "neoforge",
		PlatformVersion:     "21.1.0",
		CompatiblePlatforms: []string{"fabric", "mcdr"},
	}

	if err := ValidatePlatformSelection(
		s.Platform,
		s.CompatiblePlatforms,
	); err != nil {
		t.Fatalf("platform selection should be valid: %v", err)
	}

	result, err := BuildResult(s)
	if err != nil {
		t.Fatalf("build result: %v", err)
	}

	if result.ManifestToWrite == nil {
		t.Fatal("expected manifest")
	}
	if got := result.ManifestToWrite.Environment.ModdingPlatform; got != "neoforge" {
		t.Fatalf("expected primary platform neoforge, got %q", got)
	}
	if len(result.ManifestToWrite.Environment.CompatiblePlatforms) != 2 {
		t.Fatalf(
			"expected 2 compatible platforms, got %d",
			len(result.ManifestToWrite.Environment.CompatiblePlatforms),
		)
	}
}

func TestMultiPlatformSelection_ForgePlusMCDR(t *testing.T) {
	s := &InitFlowState{
		GameVersion:         "1.20.1",
		Platform:            "forge",
		PlatformVersion:     "47.1.0",
		CompatiblePlatforms: []string{"mcdr"},
	}

	if err := ValidatePlatformSelection(
		s.Platform,
		s.CompatiblePlatforms,
	); err != nil {
		t.Fatalf("forge+mcdr should be valid: %v", err)
	}

	result, err := BuildResult(s)
	if err != nil {
		t.Fatalf("build result: %v", err)
	}
	if result.ManifestToWrite == nil {
		t.Fatal("expected manifest")
	}
}

func TestFuzzyVersionIntent_PreservedInManifestAndSkeletalLockUntilResolved(t *testing.T) {
	s := NewInitFlowState(t.TempDir())
	s.GameVersion = "1.21.4"
	s.Platform = "fabric"
	s.PlatformVersion = "0.16.10"
	s.PackageClassifications = []TakeoverPackageClassification{
		{
			ID:      "fabric/lithium",
			Version: ">=0.12.0 <0.13.0",
			Source:  "modrinth",
			Role:    state.RoleRequired,
			Leaf:    true,
		},
	}

	result, err := BuildResult(s)
	if err != nil {
		t.Fatalf("build result: %v", err)
	}

	// Manifest preserves fuzzy intent verbatim.
	if result.ManifestToWrite == nil {
		t.Fatal("expected manifest")
	}
	if len(result.ManifestToWrite.Packages) != 1 {
		t.Fatalf(
			"expected 1 package, got %d",
			len(result.ManifestToWrite.Packages),
		)
	}
	if got := result.ManifestToWrite.Packages[0].Version; got != ">=0.12.0 <0.13.0" {
		t.Fatalf("expected fuzzy version preserved, got %q", got)
	}

	// Lock skeleton has no resolved packages yet.
	if result.LockToWrite == nil {
		t.Fatal("expected lock skeleton")
	}
	if len(result.LockToWrite.Packages) != 0 {
		t.Fatalf(
			"expected empty lock until resolution, got %d packages",
			len(result.LockToWrite.Packages),
		)
	}
}

func TestMCDRPlatform_WordingAndModelCompatibility(t *testing.T) {
	s := &InitFlowState{
		GameVersion:     "1.21.4",
		Platform:        "mcdr",
		PlatformVersion: "2.12.0",
	}

	// MCDR is a valid platform.
	if err := ValidatePlatformSelection(s.Platform, nil); err != nil {
		t.Fatalf("mcdr platform should be valid: %v", err)
	}

	if !CanProceed(s) {
		t.Error("expected CanProceed to return true for mcdr-only setup")
	}

	result, err := BuildResult(s)
	if err != nil {
		t.Fatalf("build result: %v", err)
	}

	if result.ManifestToWrite == nil {
		t.Fatal("expected manifest")
	}
	if got := result.ManifestToWrite.Environment.ModdingPlatform; got != "mcdr" {
		t.Fatalf("expected platform mcdr, got %q", got)
	}
	if result.ManifestToWrite.Environment.ModdingPlatformVersion != "2.12.0" {
		t.Fatalf(
			"expected platform version 2.12.0, got %q",
			result.ManifestToWrite.Environment.ModdingPlatformVersion,
		)
	}
}

func TestMCDRPlusFabric_ValidCoexistence(t *testing.T) {
	s := &InitFlowState{
		Platform:            "fabric",
		CompatiblePlatforms: []string{"mcdr"},
	}

	if err := ValidatePlatformSelection(
		s.Platform,
		s.CompatiblePlatforms,
	); err != nil {
		t.Fatalf("fabric+mcdr should be valid: %v", err)
	}
}

func TestMixedManagedAndIgnoredContent_ClassificationsPreservedInManifest(t *testing.T) {
	s := NewInitFlowState(t.TempDir())
	s.GameVersion = "1.21.4"
	s.Platform = "fabric"
	s.PlatformVersion = "0.16.10"
	s.PackageClassifications = []TakeoverPackageClassification{
		{
			ID:      "fabric/lithium",
			Version: "0.12.7",
			Source:  "modrinth",
			Role:    state.RoleRequired,
			Leaf:    true,
		},
		{
			ID:      "fabric/sodium",
			Version: "0.5.8",
			Source:  "modrinth",
			Role:    state.RoleTransitive,
			Leaf:    false,
		},
		{
			ID:      "fabric/manual-mod",
			Version: "1.0.0",
			Source:  "github",
			Role:    state.RoleIgnored,
			Leaf:    true,
			Pinned:  true, // pinned means manually managed, should stay ignored.
		},
	}

	result, err := BuildResult(s)
	if err != nil {
		t.Fatalf("build result: %v", err)
	}

	if len(result.ManifestToWrite.Packages) != 3 {
		t.Fatalf(
			"expected 3 packages in manifest, got %d",
			len(result.ManifestToWrite.Packages),
		)
	}

	byID := make(
		map[string]state.ManifestPackage,
		len(result.ManifestToWrite.Packages),
	)
	for _, pkg := range result.ManifestToWrite.Packages {
		byID[pkg.ID] = pkg
	}

	if got := byID["fabric/lithium"]; got.Role != state.RoleRequired {
		t.Fatalf("expected lithium required, got %q", got.Role)
	}
	if got := byID["fabric/sodium"]; got.Role != state.RoleTransitive {
		t.Fatalf("expected sodium transitive, got %q", got.Role)
	}
	if got, ok := byID["fabric/manual-mod"]; !ok {
		t.Fatal("expected manual-mod in manifest")
	} else if got.Role != state.RoleIgnored {
		t.Fatalf("expected manual-mod ignored, got %q", got.Role)
	} else if !got.Pinned {
		t.Fatal("expected manual-mod to remain pinned")
	}
}

func TestMixedManualAndManaged_OnlyManualJarsInManifestAsIgnored(t *testing.T) {
	s := NewInitFlowState(t.TempDir())
	s.GameVersion = "1.21.4"
	s.Platform = "fabric"
	s.PlatformVersion = "0.16.10"
	s.PackageClassifications = []TakeoverPackageClassification{
		{
			ID:      "fabric/lithium",
			Version: "0.12.7",
			Source:  "modrinth",
			Role:    state.RoleRequired,
			Leaf:    true,
		},
		{
			ID:      "mcdr/primebackup",
			Version: "1.12.0",
			Source:  "mcdr",
			Role:    state.RoleIgnored, // MCDR plugins not in mods dir get marked ignored.
			Leaf:    true,
		},
	}

	result, err := BuildResult(s)
	if err != nil {
		t.Fatalf("build result: %v", err)
	}

	// Both should appear in manifest with their respective roles.
	byID := make(
		map[string]state.ManifestPackage,
		len(result.ManifestToWrite.Packages),
	)
	for _, pkg := range result.ManifestToWrite.Packages {
		byID[pkg.ID] = pkg
	}

	if _, ok := byID["fabric/lithium"]; !ok {
		t.Error("expected lithium in manifest")
	}
	// MCDR plugin marked as ignored in plugins dir should still appear.
	if _, ok := byID["mcdr/primebackup"]; !ok {
		t.Error("expected MCDR plugin in manifest")
	}
}

func TestMCDRPluginDetectedAsPackage_ClassifiedCorrectly(t *testing.T) {
	pkgs := []types.Package{
		{
			Id: types.VersionedPackageRef{
				PackageRef: types.PackageRef{
					Platform: types.PlatformMCDR,
					Name:     types.BarePackageName("primebackup"),
				},
			},
			Remote: &types.PackageRemote{
				Source: types.SourceMCDR,
			},
		},
	}

	classifications := BuildTakeoverPackageClassifications(pkgs)
	if len(classifications) != 1 {
		t.Fatalf("expected 1 classification, got %d", len(classifications))
	}

	// MCDR plugins should be classified as leaves since nothing depends on them.
	if !classifications[0].Leaf {
		t.Error("expected MCDR plugin to be classified as leaf")
	}
	if classifications[0].Role != state.RoleRequired {
		t.Fatalf(
			"expected default role required for leaf, got %q",
			classifications[0].Role,
		)
	}
}

func TestTakeoverFactPrecedence_IsCorrectOrder(t *testing.T) {
	precedence := TakeoverFactPrecedence()
	if len(precedence) != 3 {
		t.Fatalf("expected 3 precedence levels, got %d", len(precedence))
	}
	if precedence[0] != FactSourceObserved {
		t.Errorf("expected observed first, got %q", precedence[0])
	}
	if precedence[1] != FactSourceUserConfirmed {
		t.Errorf("expected user confirmed second, got %q", precedence[1])
	}
	if precedence[2] != FactSourceExistingLucy {
		t.Errorf("expected existing_lucy last, got %q", precedence[2])
	}
}
