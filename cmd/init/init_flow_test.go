package init

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mclucy/lucy/state"
	"github.com/mclucy/lucy/syntax"
	"github.com/mclucy/lucy/types"
)

func TestNewInitFlowState_EmptyWorkDir(t *testing.T) {
	tmpDir := t.TempDir()
	s := NewInitFlowState(tmpDir)

	if len(s.ExistingFiles) != 0 {
		t.Errorf("expected no existing files, got %v", s.ExistingFiles)
	}
}

func TestNewInitFlowState_WithExistingConfig(t *testing.T) {
	tmpDir := t.TempDir()
	_ = filepath.Join(tmpDir, string(state.ConfigFile))
	cfg := state.ConfigDefaults()
	if err := state.WriteConfig(tmpDir, &cfg); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	s := NewInitFlowState(tmpDir)

	if !slices.Contains(s.ExistingFiles, string(state.ConfigFile)) {
		t.Errorf("expected ExistingFiles to contain %s, got %v", state.ConfigFile, s.ExistingFiles)
	}
}

func TestBuildResult_PreserveExisting(t *testing.T) {
	tmpDir := t.TempDir()
	_ = filepath.Join(tmpDir, string(state.ConfigFile))
	cfg := state.ConfigDefaults()
	if err := state.WriteConfig(tmpDir, &cfg); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	s := NewInitFlowState(tmpDir)
	s.GameVersion = "1.21.4"
	s.Platform = "none"
	s.ConflictResolution = PreserveExisting
	s.ManagedRoots = []string{"mods", "plugins"}

	result, err := BuildResult(s)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.ConfigToWrite != nil {
		t.Error("expected ConfigToWrite to be nil when preserving existing")
	}

	if !slices.Contains(result.SkippedFiles, string(state.ConfigFile)) {
		t.Errorf("expected SkippedFiles to contain %s, got %v", state.ConfigFile, result.SkippedFiles)
	}
}

func TestBuildResult_AbortOnConflict(t *testing.T) {
	tmpDir := t.TempDir()
	_ = filepath.Join(tmpDir, string(state.ConfigFile))
	cfg := state.ConfigDefaults()
	if err := state.WriteConfig(tmpDir, &cfg); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	s := NewInitFlowState(tmpDir)
	s.GameVersion = "1.21.4"
	s.Platform = "none"
	s.ConflictResolution = AbortOnConflict
	s.ManagedRoots = []string{"mods", "plugins"}

	_, err := BuildResult(s)
	if err == nil {
		t.Error("expected error when aborting on conflict with existing files")
	}

	conflictErr, ok := err.(*ErrConflict)
	if !ok {
		t.Fatalf("expected ErrConflict, got %T", err)
	}
	if conflictErr.Mode != AbortOnConflict {
		t.Errorf("expected mode AbortOnConflict, got %v", conflictErr.Mode)
	}
}

func TestBuildResult_OverwriteAll(t *testing.T) {
	tmpDir := t.TempDir()
	_ = filepath.Join(tmpDir, string(state.ConfigFile))
	cfg := state.ConfigDefaults()
	if err := state.WriteConfig(tmpDir, &cfg); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	s := NewInitFlowState(tmpDir)
	s.GameVersion = "1.21.4"
	s.Platform = "none"
	s.ConflictResolution = OverwriteAll
	s.ManagedRoots = []string{"mods", "plugins"}

	result, err := BuildResult(s)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.ConfigToWrite == nil {
		t.Error("expected ConfigToWrite to be set when OverwriteAll")
	}

	if !slices.Contains(result.WrittenFiles, string(state.ConfigFile)) {
		t.Errorf("expected WrittenFiles to contain %s, got %v", state.ConfigFile, result.WrittenFiles)
	}
}

func TestBuildResult_PersistsCompatiblePlatforms(t *testing.T) {
	tmpDir := t.TempDir()
	s := NewInitFlowState(tmpDir)
	s.GameVersion = "1.21.4"
	s.Platform = "neoforge"
	s.PlatformVersion = "21.1.0"
	s.CompatiblePlatforms = []string{"fabric", "mcdr", "sinytra"}
	s.ManagedRoots = []string{"mods", "plugins"}

	result, err := BuildResult(s)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.ManifestToWrite == nil {
		t.Fatal("expected manifest to be written")
	}
	want := []string{"fabric", "mcdr", "sinytra"}
	if len(result.ManifestToWrite.Environment.CompatiblePlatforms) != len(want) {
		t.Fatalf("expected %d compatible platforms, got %d", len(want), len(result.ManifestToWrite.Environment.CompatiblePlatforms))
	}
	for i, platform := range want {
		if result.ManifestToWrite.Environment.CompatiblePlatforms[i] != platform {
			t.Fatalf("compatible platform %d mismatch: got %q want %q", i, result.ManifestToWrite.Environment.CompatiblePlatforms[i], platform)
		}
	}
}

func TestBuildResultPreserveManifestStillPopulatesLockMetadataFromExistingManifest(t *testing.T) {
	tmpDir := t.TempDir()

	manifest := state.ManifestDefaults()
	manifest.Environment.GameVersion = "1.21.4"
	manifest.Environment.ModdingPlatform = "fabric"
	manifest.Environment.ModdingPlatformVersion = "0.16.10"
	if err := state.WriteManifest(tmpDir, &manifest); err != nil {
		t.Fatalf("failed to write manifest: %v", err)
	}

	s := NewInitFlowState(tmpDir)
	s.ManagedRoots = []string{"mods"}
	s.ConflictResolution = PreserveExisting

	result, err := BuildResult(s)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ManifestToWrite != nil {
		t.Fatal("expected manifest write to be skipped in preserve mode")
	}
	if result.LockToWrite == nil {
		t.Fatal("expected lock skeleton to be written")
	}

	manifestBytes, err := state.SerializeManifest(&manifest)
	if err != nil {
		t.Fatalf("serialize manifest failed: %v", err)
	}
	sum := sha256.Sum256(manifestBytes)
	wantFingerprint := "sha256:" + hex.EncodeToString(sum[:])

	if result.LockToWrite.ManifestFingerprint != wantFingerprint {
		t.Fatalf("manifest fingerprint mismatch: got %q want %q", result.LockToWrite.ManifestFingerprint, wantFingerprint)
	}
	if result.LockToWrite.GameVersion != manifest.Environment.GameVersion {
		t.Fatalf("game version mismatch: got %q want %q", result.LockToWrite.GameVersion, manifest.Environment.GameVersion)
	}
	if result.LockToWrite.Platform != manifest.Environment.ModdingPlatform {
		t.Fatalf("platform mismatch: got %q want %q", result.LockToWrite.Platform, manifest.Environment.ModdingPlatform)
	}
	if result.LockToWrite.PlatformVersion != manifest.Environment.ModdingPlatformVersion {
		t.Fatalf("platform version mismatch: got %q want %q", result.LockToWrite.PlatformVersion, manifest.Environment.ModdingPlatformVersion)
	}
	if err := state.ValidateLock(*result.LockToWrite); err != nil {
		t.Fatalf("expected preserve-mode lock skeleton to validate: %v", err)
	}
}

func TestCanProceed_EmptyGameVersion(t *testing.T) {
	s := &InitFlowState{
		GameVersion:  "",
		Platform:     "none",
		ManagedRoots: []string{"mods", "plugins"},
	}

	if CanProceed(s) {
		t.Error("expected CanProceed to return false when GameVersion is empty")
	}
}

func TestCanProceed_EmptyManagedRoots(t *testing.T) {
	s := &InitFlowState{
		GameVersion:  "1.21.4",
		Platform:     "none",
		ManagedRoots: nil,
	}

	if CanProceed(s) {
		t.Error("expected CanProceed to return false when ManagedRoots is empty")
	}
}

func TestCanProceed_ValidState(t *testing.T) {
	s := &InitFlowState{
		GameVersion:  "1.21.4",
		Platform:     "none",
		ManagedRoots: []string{"mods", "plugins"},
	}

	if !CanProceed(s) {
		t.Error("expected CanProceed to return true for valid state")
	}
}

func TestCanProceed_ValidWithMultipleRoots(t *testing.T) {
	s := &InitFlowState{
		GameVersion:  "1.21.4",
		Platform:     "none",
		ManagedRoots: []string{"mods", "plugins", "config"},
	}

	if !CanProceed(s) {
		t.Error("expected CanProceed to return true for valid state with multiple roots")
	}
}

func TestConflictMode_String(t *testing.T) {
	tests := []struct {
		mode ConflictMode
		want string
	}{
		{PreserveExisting, "preserve"},
		{AbortOnConflict, "abort"},
		{OverwriteAll, "overwrite"},
	}

	for _, tc := range tests {
		if string(tc.mode) != tc.want {
			t.Errorf("expected %q, got %q", tc.want, tc.mode)
		}
	}
}

func TestDiscoverServerDefaults_UsesProbeObservedTakeoverCandidates(t *testing.T) {
	tmpDir := t.TempDir()
	copyFile(
		t,
		filepath.Join("..", "..", "probe", "internal", "detector", "testdata", "fabric", "fabric-server-launch.jar"),
		filepath.Join(tmpDir, "fabric-server-launch.jar"),
	)

	defaults := DiscoverServerDefaults(tmpDir)

	if defaults.Platform != string(types.PlatformFabric) {
		t.Fatalf("expected platform %q, got %q", types.PlatformFabric, defaults.Platform)
	}
	if defaults.PlatformVersion == "" {
		t.Fatal("expected probe-derived platform version to be populated")
	}
	if !slices.Contains(defaults.ManagedRoots, "mods") {
		t.Fatalf("expected mods managed root from probe topology, got %v", defaults.ManagedRoots)
	}
	if !slices.Contains(defaults.DetectedPackages, "fabric/fabric") {
		t.Fatalf("expected runtime identity takeover candidate in detected packages, got %v", defaults.DetectedPackages)
	}
	if defaults.Confidence == ConfidenceNone {
		t.Fatal("expected non-empty discovery confidence")
	}
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", dst, err)
	}
}

func TestValidatePlatformSelectionRejectsImpossibleCombination(t *testing.T) {
	err := ValidatePlatformSelection("fabric", []string{"sinytra"})
	if err == nil {
		t.Fatal("expected impossible platform combination to fail")
	}
}

func TestBuildSummaryShowsPrimaryAndCompatiblePlatforms(t *testing.T) {
	s := &InitFlowState{
		GameVersion:         "1.21.4",
		Platform:            "neoforge",
		PlatformVersion:     "21.1.0",
		CompatiblePlatforms: []string{"fabric", "mcdr", "sinytra"},
		ManagedRoots:        []string{"mods", "plugins"},
		ConflictResolution:  PreserveExisting,
	}

	summary := buildSummary(s)
	if want := "  Primary runtime: neoforge"; !containsLine(summary, want) {
		t.Fatalf("expected summary to contain %q, got:\n%s", want, summary)
	}
	if want := "  Compatible with: fabric, mcdr, sinytra"; !containsLine(summary, want) {
		t.Fatalf("expected summary to contain %q, got:\n%s", want, summary)
	}
}

func TestBuildTakeoverPackageClassificationsSurfacesNonLeafDependencies(t *testing.T) {
	packages := []types.Package{
		testObservedPackage(
			"fabric/lithium@0.12.7",
			types.SourceModrinth,
			[]types.Dependency{{Id: types.PackageId{Platform: types.PlatformFabric, Name: "fabric-api", Version: types.VersionAny}, Mandatory: true}},
		),
		testObservedPackage(
			"fabric/fabric-api@0.119.2+1.21.5",
			types.SourceModrinth,
			[]types.Dependency{{Id: types.PackageId{Platform: types.PlatformFabric, Name: "cloth-config", Version: types.VersionAny}, Mandatory: true}},
		),
		testObservedPackage("fabric/cloth-config@15.0.140", types.SourceModrinth, nil),
	}

	classifications := BuildTakeoverPackageClassifications(packages)
	if len(classifications) != 3 {
		t.Fatalf("expected 3 classified packages, got %d", len(classifications))
	}

	byID := make(map[string]TakeoverPackageClassification, len(classifications))
	for _, classification := range classifications {
		byID[classification.ID] = classification
	}

	if got := byID["fabric/lithium"]; !got.Leaf || got.Role != state.RoleRequired {
		t.Fatalf("expected lithium to be a required leaf, got %#v", got)
	}
	if got := byID["fabric/fabric-api"]; got.Leaf || got.Role != state.RoleTransitive {
		t.Fatalf("expected fabric-api to be a transitive non-leaf, got %#v", got)
	}
	if got := byID["fabric/cloth-config"]; got.Leaf || got.Role != state.RoleTransitive {
		t.Fatalf("expected cloth-config to be a transitive non-leaf, got %#v", got)
	}
	if got := byID["fabric/fabric-api"]; !slices.Equal(got.RequiredBy, []string{"fabric/lithium"}) {
		t.Fatalf("expected fabric-api required-by chain, got %#v", got.RequiredBy)
	}
	if got := byID["fabric/cloth-config"]; !slices.Equal(got.RequiredBy, []string{"fabric/fabric-api"}) {
		t.Fatalf("expected cloth-config required-by chain, got %#v", got.RequiredBy)
	}
}

func TestBuildResultIncludesClassifiedPackagesInManifest(t *testing.T) {
	tmpDir := t.TempDir()
	s := NewInitFlowState(tmpDir)
	s.GameVersion = "1.21.4"
	s.Platform = "fabric"
	s.PlatformVersion = "0.16.10"
	s.ManagedRoots = []string{"mods"}
	s.PackageClassifications = []TakeoverPackageClassification{
		{ID: "fabric/lithium", Version: "0.12.7", Source: "modrinth", Role: state.RoleRequired, Leaf: true},
		{ID: "fabric/fabric-api", Version: "0.119.2+1.21.5", Source: "modrinth", Role: state.RoleTransitive},
		{ID: "fabric/cloth-config", Version: "15.0.140", Source: "modrinth", Role: state.RoleIgnored},
	}

	result, err := BuildResult(s)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ManifestToWrite == nil {
		t.Fatal("expected manifest to be written")
	}
	if len(result.ManifestToWrite.Packages) != 3 {
		t.Fatalf("expected 3 manifest packages, got %d", len(result.ManifestToWrite.Packages))
	}

	byID := make(map[string]state.ManifestPackage, len(result.ManifestToWrite.Packages))
	for _, pkg := range result.ManifestToWrite.Packages {
		byID[pkg.ID] = pkg
	}
	if got := byID["fabric/lithium"].Role; got != state.RoleRequired {
		t.Fatalf("expected lithium role required, got %q", got)
	}
	if got := byID["fabric/fabric-api"].Role; got != state.RoleTransitive {
		t.Fatalf("expected fabric-api role transitive, got %q", got)
	}
	if got := byID["fabric/cloth-config"].Role; got != state.RoleIgnored {
		t.Fatalf("expected cloth-config role ignored, got %q", got)
	}
}

func TestBuildPackageClassificationDescriptionDistinguishesNonLeafNodes(t *testing.T) {
	s := &InitFlowState{
		PackageClassifications: []TakeoverPackageClassification{
			{ID: "fabric/lithium", Version: "0.12.7", Role: state.RoleRequired, Leaf: true},
			{ID: "fabric/fabric-api", Version: "0.119.2+1.21.5", Role: state.RoleTransitive, RequiredBy: []string{"fabric/lithium"}},
		},
	}

	description := buildPackageClassificationDescription(s)
	if !strings.Contains(description, "[leaf]") {
		t.Fatalf("expected package classification description to mark leaf nodes, got:\n%s", description)
	}
	if !strings.Contains(description, "[dependency]") {
		t.Fatalf("expected package classification description to mark non-leaf dependency nodes, got:\n%s", description)
	}
}

func testObservedPackage(id string, source types.Source, deps []types.Dependency) types.Package {
	pkgID, err := syntax.Parse(id)
	if err != nil {
		panic(err)
	}
	pkg := types.Package{Id: pkgID}
	if source != types.SourceUnknown {
		pkg.Remote = &types.PackageRemote{Source: source}
	}
	if deps != nil {
		pkg.Dependencies = &types.PackageDependencies{Value: deps, Authentic: true}
	}
	return pkg
}

func containsLine(text, want string) bool {
	for line := range strings.SplitSeq(text, "\n") {
		if line == want {
			return true
		}
	}
	return false
}

func TestBuildSummaryShowsObservedFacts(t *testing.T) {
	s := &InitFlowState{
		GameVersion:        "1.21.4",
		Platform:           "fabric",
		PlatformVersion:    "0.16.10",
		ManagedRoots:       []string{"mods"},
		ConflictResolution: PreserveExisting,
		DiscoveredDefaults: DiscoveredDefaults{
			Confidence:       ConfidenceHigh,
			GameVersion:      "1.21.4",
			Platform:         "fabric",
			PlatformVersion:  "0.16.10",
			ManagedRoots:     []string{"mods"},
			DetectedPackages: []string{"fabric/lithium", "fabric/fabric-api"},
		},
	}

	summary := buildSummary(s)
	if !strings.Contains(summary, "Observed server facts") {
		t.Fatalf("expected summary to contain observed section header, got:\n%s", summary)
	}
	if !strings.Contains(summary, "Proposed manifest intent") {
		t.Fatalf("expected summary to contain proposed section header, got:\n%s", summary)
	}
	if want := "  Confidence:    high"; !containsLine(summary, want) {
		t.Fatalf("expected summary to contain %q, got:\n%s", want, summary)
	}
	if want := "  Packages:      2 detected"; !containsLine(summary, want) {
		t.Fatalf("expected summary to contain %q, got:\n%s", want, summary)
	}
}

func TestBuildSummaryShowsConflictsWhenObservedDiffersFromExistingLucy(t *testing.T) {
	s := &InitFlowState{
		GameVersion:        "1.21.4",
		Platform:           "fabric",
		PlatformVersion:    "0.16.10",
		ManagedRoots:       []string{"mods"},
		ConflictResolution: PreserveExisting,
		ExistingFiles:      []string{".lucy/manifest.toml"},
		DiscoveredDefaults: DiscoveredDefaults{
			Confidence:      ConfidenceHigh,
			GameVersion:     "1.21.4",
			Platform:        "fabric",
			PlatformVersion: "0.16.10",
			ExistingLucy: ExistingLucyHints{
				ManifestPresent: true,
				GameVersion:     "1.20.6",
				Platform:        "neoforge",
				PlatformVersion: "21.0.0",
			},
		},
	}

	summary := buildSummary(s)
	if !strings.Contains(summary, "Conflicts") {
		t.Fatalf("expected conflicts section in summary when observed differs from existing .lucy, got:\n%s", summary)
	}
	if !strings.Contains(summary, `observed "1.21.4"`) {
		t.Fatalf("expected game version divergence in conflicts section, got:\n%s", summary)
	}
	if !strings.Contains(summary, `observed "fabric"`) {
		t.Fatalf("expected platform divergence in conflicts section, got:\n%s", summary)
	}
}

func TestBuildSummaryNoConflictsSectionWhenObservedMatchesExisting(t *testing.T) {
	s := &InitFlowState{
		GameVersion:        "1.21.4",
		Platform:           "fabric",
		ManagedRoots:       []string{"mods"},
		ConflictResolution: PreserveExisting,
		DiscoveredDefaults: DiscoveredDefaults{
			Confidence:  ConfidenceHigh,
			GameVersion: "1.21.4",
			Platform:    "fabric",
			ExistingLucy: ExistingLucyHints{
				ManifestPresent: true,
				GameVersion:     "1.21.4",
				Platform:        "fabric",
			},
		},
	}

	summary := buildSummary(s)
	if strings.Contains(summary, "Conflicts") {
		t.Fatalf("expected no conflicts section when observed matches existing .lucy, got:\n%s", summary)
	}
}
