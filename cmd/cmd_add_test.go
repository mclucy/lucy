package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"testing"

	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/state"
	"github.com/mclucy/lucy/syntax"
	"github.com/mclucy/lucy/types"
)

func TestBuildUpdatedManifestPreservesFuzzyIntentAndPromotesRequired(t *testing.T) {
	existing := state.ManifestDefaults()
	existing.Packages = []state.ManifestPackage{{
		ID:       "fabric/lithium",
		Version:  "0.12.7",
		Source:   "modrinth",
		Role:     state.RoleTransitive,
		Side:     state.SideServer,
		Optional: true,
		Pinned:   true,
	}}

	requested := []types.PackageId{
		mustParsePackageID(t, "fabric/lithium@>=0.12.0 <0.13.0"),
		mustParsePackageID(t, "fabric/fabric-api"),
	}

	updated := buildUpdatedManifest(&existing, requested)
	if updated == nil {
		t.Fatal("expected updated manifest")
	}

	byID := make(map[string]state.ManifestPackage, len(updated.Packages))
	for _, pkg := range updated.Packages {
		byID[pkg.ID] = pkg
	}

	lithium := byID["fabric/lithium"]
	if lithium.Version != ">=0.12.0 <0.13.0" {
		t.Fatalf("expected fuzzy version intent to be preserved, got %q", lithium.Version)
	}
	if lithium.Role != state.RoleRequired {
		t.Fatalf("expected lithium role required, got %q", lithium.Role)
	}
	if lithium.Side != state.SideServer || !lithium.Optional || !lithium.Pinned {
		t.Fatalf("expected existing package metadata to be preserved, got %#v", lithium)
	}

	fabricAPI := byID["fabric/fabric-api"]
	if fabricAPI.Version != types.VersionCompatible.String() {
		t.Fatalf("expected omitted version to stay fuzzy-compatible, got %q", fabricAPI.Version)
	}
	if fabricAPI.Role != state.RoleRequired {
		t.Fatalf("expected explicit add to become required intent, got %q", fabricAPI.Role)
	}
	if fabricAPI.Source != "auto" {
		t.Fatalf("expected default source auto, got %q", fabricAPI.Source)
	}
}

func TestBuildUpdatedLockRefreshesExactClosureAndPrunesStalePackages(t *testing.T) {
	workDir := t.TempDir()
	manifest := state.ManifestDefaults()
	manifest.Environment.GameVersion = "1.21.1"
	manifest.Environment.Platform = "fabric"
	manifest.Environment.PlatformVersion = "0.16.10"
	manifest.Packages = []state.ManifestPackage{{
		ID:      "fabric/lithium",
		Version: "latest",
		Source:  "auto",
		Role:    state.RoleRequired,
		Side:    state.SideServer,
	}}

	existingLock := state.NewLock()
	existingLock.ManifestFingerprint = "sha256:stale"
	existingLock.GameVersion = "1.21.1"
	existingLock.Platform = "fabric"
	existingLock.PlatformVersion = "0.16.9"
	existingLock.Packages = []state.LockedPackage{{
		ID:            "fabric/stale-dependency",
		Version:       "1.0.0",
		Source:        "modrinth",
		URL:           "https://example.invalid/stale.jar",
		Filename:      "stale.jar",
		Hash:          "stalehash",
		HashAlgorithm: "sha512",
		InstallPath:   "mods/stale.jar",
		Side:          "server",
		Provenance:    []string{"root"},
		Requester:     "root",
	}}
	existingLock.Bundles = []state.LockedBundle{{
		Name:        "defaults",
		Type:        "config",
		Hash:        "bundlehash",
		InstallPath: "config/defaults.zip",
	}}

	result := &install.Result{
		Installed: []types.Package{
			lockedResultPackage(t, workDir, "fabric/lithium@0.12.9+mc1.21.1", "lithium.jar"),
			lockedResultPackage(t, workDir, "fabric/fabric-api@0.119.2+1.21.5", "fabric-api.jar"),
		},
		Provenance: map[string][]string{
			"fabric/lithium":    {"root"},
			"fabric/fabric-api": {"root", "fabric/lithium@0.12.9+mc1.21.1"},
		},
	}

	updated := buildUpdatedLock(workDir, &manifest, &existingLock, result)
	if updated == nil {
		t.Fatal("expected updated lock")
	}

	if len(updated.Packages) != 2 {
		t.Fatalf("expected exact lock closure to replace stale packages, got %d entries", len(updated.Packages))
	}
	if updated.Packages[0].ID != "fabric/fabric-api" || updated.Packages[1].ID != "fabric/lithium" {
		t.Fatalf("expected lock packages to be canonically sorted, got %#v", updated.Packages)
	}
	for _, pkg := range updated.Packages {
		if pkg.ID == "fabric/stale-dependency" {
			t.Fatal("expected stale lock entry to be pruned during refresh")
		}
	}

	manifestBytes, err := state.SerializeManifest(&manifest)
	if err != nil {
		t.Fatalf("serialize manifest failed: %v", err)
	}
	sum := sha256.Sum256(manifestBytes)
	wantFingerprint := "sha256:" + hex.EncodeToString(sum[:])
	if updated.ManifestFingerprint != wantFingerprint {
		t.Fatalf("manifest fingerprint mismatch: got %q want %q", updated.ManifestFingerprint, wantFingerprint)
	}
	if updated.PlatformVersion != manifest.Environment.PlatformVersion {
		t.Fatalf("expected lock metadata to refresh from manifest, got %q", updated.PlatformVersion)
	}
	if len(updated.Bundles) != 1 || updated.Bundles[0] != existingLock.Bundles[0] {
		t.Fatalf("expected unrelated lock bundles to be preserved, got %#v", updated.Bundles)
	}
	if err := state.ValidateLock(*updated); err != nil {
		t.Fatalf("expected refreshed lock to validate: %v", err)
	}
}

func mustParsePackageID(t *testing.T, raw string) types.PackageId {
	t.Helper()
	id, err := syntax.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return id
}

func lockedResultPackage(t *testing.T, workDir, rawID, filename string) types.Package {
	t.Helper()
	id := mustParsePackageID(t, rawID)
	return types.Package{
		Id: id,
		Local: &types.PackageInstallation{
			Path: filepath.Join(workDir, "mods", filename),
		},
		Remote: &types.PackageRemote{
			Source:        types.SourceModrinth,
			FileUrl:       "https://example.invalid/" + filename,
			Filename:      filename,
			Hash:          "deadbeef",
			HashAlgorithm: "sha512",
		},
	}
}
