package state

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/mclucy/lucy/types"
)

func TestManifestRoundTrip(t *testing.T) {
	manifestText := []byte(`{
	  "format_version": "v1",
	  "environment": {
	    "game_version": "1.21.1",
	    "modding_platform": "neoforge",
	    "modding_platform_version": "21.1.0",
	    "compatible_platforms": ["fabric", "mcdr", "sinytra"]
	  },
	  "packages": [
	    {
	      "id": "neoforge/connector",
	      "version": "compatible",
	      "source": "modrinth",
	      "role": "required",
	      "side": "server",
	      "optional": false,
	      "pinned": false
	    }
	  ],
	  "bundles": [
	    {
	      "name": "server-config",
	      "type": "config",
	      "path": "config",
	      "source": "./defaults/config",
	      "optional": false
	    }
	  ]
	}`)

	var manifest Manifest
	if err := manifest.Unmarshal(manifestText); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	first, err := manifest.Marshal()
	if err != nil {
		t.Fatalf("first marshal failed: %v", err)
	}

	var parsed Manifest
	if err := parsed.Unmarshal(first); err != nil {
		t.Fatalf("re-unmarshal failed: %v", err)
	}

	second, err := parsed.Marshal()
	if err != nil {
		t.Fatalf("second marshal failed: %v", err)
	}

	if !bytes.Equal(first, second) {
		t.Fatalf("round-trip produced different output:\nfirst:\n%s\nsecond:\n%s", first, second)
	}

	if err := ValidateManifest(parsed); err != nil {
		t.Fatalf("round-trip manifest should validate: %v", err)
	}
}

func TestManifestPreservesPackageSides(t *testing.T) {
	manifest := ManifestDefaults()
	manifest.Environment.GameVersion = "1.21.1"
	manifest.Environment.ModdingPlatform = "fabric"
	manifest.Environment.ModdingPlatformVersion = "0.16.10"
	manifest.Packages = []ManifestPackage{
		{ID: "fabric/lithium", Version: "compatible", Source: "auto", Role: RoleRequired, Side: SideServer},
		{ID: "fabric/sodium", Version: "latest", Source: "modrinth", Role: RoleTransitive, Side: SideClient, Optional: true},
		{ID: "fabric/fabric-api", Version: "0.119.2+1.21.5", Source: "github", Role: RoleIgnored, Side: SideBoth, Pinned: true},
	}

	data, err := SerializeManifest(&manifest)
	if err != nil {
		t.Fatalf("serialize manifest failed: %v", err)
	}

	reparsed, err := ParseManifest(data)
	if err != nil {
		t.Fatalf("reparse manifest failed: %v", err)
	}

	gotSides := []ManifestSide{reparsed.Packages[0].Side, reparsed.Packages[1].Side, reparsed.Packages[2].Side}
	wantSides := []ManifestSide{SideServer, SideClient, SideBoth}
	for i := range wantSides {
		if gotSides[i] != wantSides[i] {
			t.Fatalf("package %d side mismatch: got %q want %q", i, gotSides[i], wantSides[i])
		}
	}

	if !reparsed.Packages[1].Optional {
		t.Fatalf("expected client package to remain optional")
	}
	if !reparsed.Packages[2].Pinned {
		t.Fatalf("expected pinned flag to remain true")
	}
}

func TestManifestPreservesFuzzyVersionIntentVerbatim(t *testing.T) {
	manifest := ManifestDefaults()
	manifest.Environment.GameVersion = "1.21.1"
	manifest.Environment.ModdingPlatform = "fabric"
	manifest.Environment.ModdingPlatformVersion = "0.16.10"
	manifest.Packages = []ManifestPackage{{
		ID:      "fabric/lithium",
		Version: ">=0.12.0 <0.13.0",
		Source:  "auto",
		Role:    RoleRequired,
		Side:    SideServer,
	}}

	data, err := SerializeManifest(&manifest)
	if err != nil {
		t.Fatalf("serialize manifest failed: %v", err)
	}

	reparsed, err := ParseManifest(data)
	if err != nil {
		t.Fatalf("reparse manifest failed: %v", err)
	}
	if got := reparsed.Packages[0].Version; got != ">=0.12.0 <0.13.0" {
		t.Fatalf("expected fuzzy selector to survive round trip, got %q", got)
	}
	if reparsed.Packages[0].Version == "0.12.9" {
		t.Fatal("manifest intent must not be rewritten to an exact resolved version")
	}
}

func TestManifestSupportsCompatiblePlatformsAndPreservesIntent(t *testing.T) {
	manifest := ManifestDefaults()
	manifest.Environment.GameVersion = "1.21.1"
	manifest.Environment.ModdingPlatform = "neoforge"
	manifest.Environment.ModdingPlatformVersion = "21.1.0"
	manifest.Environment.CompatiblePlatforms = []string{"fabric", "mcdr", "sinytra"}

	data, err := SerializeManifest(&manifest)
	if err != nil {
		t.Fatalf("serialize manifest failed: %v", err)
	}

	reparsed, err := ParseManifest(data)
	if err != nil {
		t.Fatalf("reparse manifest failed: %v", err)
	}

	if got, want := reparsed.Environment.CompatiblePlatforms, []string{"fabric", "mcdr", "sinytra"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("round-trip environment compatible platforms mismatch: got %#v want %#v", got, want)
	}
	if !bytes.Contains(data, []byte(`"compatible_platforms": [`)) {
		t.Fatalf("serialized manifest missing compatible_platforms: %s", data)
	}
}

func TestManifestRejectsIncompatibleEnvironmentCompatiblePlatforms(t *testing.T) {
	manifest := ManifestDefaults()
	manifest.Environment.ModdingPlatform = "fabric"
	manifest.Environment.CompatiblePlatforms = []string{"sinytra"}

	err := ValidateManifest(manifest)
	if err == nil {
		t.Fatal("expected manifest validation to reject incompatible environment compatible platforms")
	}
	if got := err.Error(); got == "" || !bytes.Contains([]byte(got), []byte("environment.compatible_platforms")) {
		t.Fatalf("expected compatible-platform validation error, got %v", err)
	}
}

func TestManifestBundlesRemainSeparateFromPackages(t *testing.T) {
	manifest := ManifestDefaults()
	manifest.Environment.GameVersion = "1.20.6"
	manifest.Environment.ModdingPlatform = "none"
	manifest.Packages = []ManifestPackage{{
		ID:      "none/luckperms",
		Version: "latest",
		Source:  "curseforge",
		Role:    RoleRequired,
		Side:    SideServer,
	}}
	manifest.Bundles = []ManifestBundle{
		{Name: "default-config", Type: BundleTypeConfig, Path: "config/luckperms", Source: "./overlays/luckperms"},
		{Name: "vanilla-datapack", Type: BundleTypeDatapack, Path: "world/datapacks/vanilla", Source: "./overlays/vanilla", Optional: true},
	}

	if err := ValidateManifest(manifest); err != nil {
		t.Fatalf("bundled manifest should validate: %v", err)
	}
	if manifest.Packages[0].ID == manifest.Bundles[0].Name {
		t.Fatalf("package identity space should remain separate from bundle names")
	}
	if manifest.Bundles[0].Type != BundleTypeConfig || manifest.Bundles[1].Type != BundleTypeDatapack {
		t.Fatalf("unexpected bundle types: %#v", manifest.Bundles)
	}
}

func TestManifestDefaults(t *testing.T) {
	manifest := ManifestDefaults()

	if manifest.FormatVersion != "v1" {
		t.Fatalf("expected format version v1, got %q", manifest.FormatVersion)
	}
	if manifest.Environment.ModdingPlatform != "" {
		t.Fatalf("expected default modding platform empty, got %q", manifest.Environment.ModdingPlatform)
	}
	if len(manifest.Environment.CompatiblePlatforms) != 0 {
		t.Fatalf("expected no compatible platforms by default, got %#v", manifest.Environment.CompatiblePlatforms)
	}
	if len(manifest.Environment.DeclaredCapabilities) != 0 {
		t.Fatalf("expected no declared capabilities by default, got %#v", manifest.Environment.DeclaredCapabilities)
	}
	if len(manifest.Packages) != 0 || len(manifest.Bundles) != 0 {
		t.Fatalf("expected empty package and bundle declarations by default")
	}

	if err := ValidateManifest(manifest); err != nil {
		t.Fatalf("default manifest should validate: %v", err)
	}
}

func TestUpdateManifestRolesForAddPromotesExplicitRequestsAndPreservesIgnored(t *testing.T) {
	manifest := &Manifest{
		FormatVersion: ManifestDefaults().FormatVersion,
		Environment: ManifestEnvironment{
			ModdingPlatform: string(types.PlatformFabric),
		},
		Packages: []ManifestPackage{
			{ID: "fabric/kept-root", Version: "compatible", Source: "auto", Role: RoleRequired, Side: SideBoth},
			{ID: "fabric/manual-jar", Version: "1.0.0", Source: "github", Role: RoleIgnored, Side: SideBoth, Pinned: true},
		},
	}
	lock := &Lock{Packages: []LockedPackage{
		{ID: "fabric/kept-root", Version: "1.0.0", Source: "modrinth", URL: "https://example.com/kept-root.jar", Filename: "kept-root.jar", Hash: "abc", HashAlgorithm: "sha1", InstallPath: "mods/kept-root.jar", Side: "both", Provenance: []string{"root"}, Requester: "root"},
		{ID: "fabric/kept-dependency", Version: "1.1.0", Source: "modrinth", URL: "https://example.com/kept-dependency.jar", Filename: "kept-dependency.jar", Hash: "def", HashAlgorithm: "sha1", InstallPath: "mods/kept-dependency.jar", Side: "both", Provenance: []string{"root", "fabric/kept-root@1.0.0"}, Requester: "fabric/kept-root"},
		{ID: "fabric/new-root", Version: "2.0.0", Source: "modrinth", URL: "https://example.com/new-root.jar", Filename: "new-root.jar", Hash: "ghi", HashAlgorithm: "sha1", InstallPath: "mods/new-root.jar", Side: "both", Provenance: []string{"root"}, Requester: "root"},
		{ID: "fabric/new-dependency", Version: "2.1.0", Source: "modrinth", URL: "https://example.com/new-dependency.jar", Filename: "new-dependency.jar", Hash: "jkl", HashAlgorithm: "sha1", InstallPath: "mods/new-dependency.jar", Side: "both", Provenance: []string{"root", "fabric/new-root@2.0.0"}, Requester: "fabric/new-root"},
	}}

	updated := UpdateManifestRolesForAdd(manifest, []types.PackageId{{Platform: types.PlatformFabric, Name: "new-root", Version: types.VersionLatest}}, lock)

	if len(updated.Packages) != 5 {
		t.Fatalf("expected 5 manifest packages after add, got %d", len(updated.Packages))
	}

	byID := make(map[string]ManifestPackage, len(updated.Packages))
	for _, pkg := range updated.Packages {
		byID[pkg.ID] = pkg
	}

	if got := byID["fabric/kept-root"]; got.Role != RoleRequired || got.Version != "compatible" {
		t.Fatalf("expected existing required root to remain required with manifest intent preserved, got %#v", got)
	}
	if got := byID["fabric/new-root"]; got.Role != RoleRequired || got.Version != types.VersionLatest.String() {
		t.Fatalf("expected added root to become required with requested intent preserved, got %#v", got)
	}
	if got := byID["fabric/kept-dependency"]; got.Role != RoleTransitive {
		t.Fatalf("expected existing dependency to remain transitive, got %#v", got)
	}
	if got := byID["fabric/new-dependency"]; got.Role != RoleTransitive {
		t.Fatalf("expected new dependency to be transitive, got %#v", got)
	}
	if got := byID["fabric/manual-jar"]; got.Role != RoleIgnored || !got.Pinned || got.Version != "1.0.0" {
		t.Fatalf("expected ignored package to remain untouched, got %#v", got)
	}
}

func TestUpdateManifestRolesForRemovePrunesOrphanedTransitivesAndKeepsIgnored(t *testing.T) {
	manifest := &Manifest{
		FormatVersion: ManifestDefaults().FormatVersion,
		Environment: ManifestEnvironment{
			ModdingPlatform: string(types.PlatformFabric),
		},
		Packages: []ManifestPackage{
			{ID: "fabric/root-a", Version: "compatible", Source: "auto", Role: RoleRequired, Side: SideBoth},
			{ID: "fabric/root-b", Version: "compatible", Source: "auto", Role: RoleRequired, Side: SideBoth},
			{ID: "fabric/dependency-a", Version: "1.0.0", Source: "modrinth", Role: RoleTransitive, Side: SideBoth},
			{ID: "fabric/dependency-b", Version: "1.0.0", Source: "modrinth", Role: RoleTransitive, Side: SideBoth},
			{ID: "fabric/manual-jar", Version: "1.0.0", Source: "github", Role: RoleIgnored, Side: SideBoth},
		},
	}
	lock := &Lock{Packages: []LockedPackage{
		{ID: "fabric/root-a", Version: "1.0.0", Source: "modrinth", URL: "https://example.com/root-a.jar", Filename: "root-a.jar", Hash: "aaa", HashAlgorithm: "sha1", InstallPath: "mods/root-a.jar", Side: "both", Provenance: []string{"root"}, Requester: "root"},
		{ID: "fabric/root-b", Version: "1.0.0", Source: "modrinth", URL: "https://example.com/root-b.jar", Filename: "root-b.jar", Hash: "bbb", HashAlgorithm: "sha1", InstallPath: "mods/root-b.jar", Side: "both", Provenance: []string{"root"}, Requester: "root"},
		{ID: "fabric/dependency-a", Version: "1.0.0", Source: "modrinth", URL: "https://example.com/dependency-a.jar", Filename: "dependency-a.jar", Hash: "ccc", HashAlgorithm: "sha1", InstallPath: "mods/dependency-a.jar", Side: "both", Provenance: []string{"root", "fabric/root-a@1.0.0"}, Requester: "fabric/root-a"},
		{ID: "fabric/dependency-b", Version: "1.0.0", Source: "modrinth", URL: "https://example.com/dependency-b.jar", Filename: "dependency-b.jar", Hash: "ddd", HashAlgorithm: "sha1", InstallPath: "mods/dependency-b.jar", Side: "both", Provenance: []string{"root", "fabric/root-b@1.0.0"}, Requester: "fabric/root-b"},
	}}

	updated := UpdateManifestRolesForRemove(manifest, []types.PackageId{{Platform: types.PlatformFabric, Name: "root-a", Version: types.VersionCompatible}, {Platform: types.PlatformFabric, Name: "manual-jar", Version: types.VersionCompatible}}, lock)

	if len(updated.Packages) != 3 {
		t.Fatalf("expected 3 manifest packages after remove, got %d", len(updated.Packages))
	}

	byID := make(map[string]ManifestPackage, len(updated.Packages))
	for _, pkg := range updated.Packages {
		byID[pkg.ID] = pkg
	}

	if _, ok := byID["fabric/root-a"]; ok {
		t.Fatalf("expected removed required root to disappear from manifest, got %#v", byID["fabric/root-a"])
	}
	if _, ok := byID["fabric/dependency-a"]; ok {
		t.Fatalf("expected orphaned transitive dependency to be pruned, got %#v", byID["fabric/dependency-a"])
	}
	if got := byID["fabric/root-b"]; got.Role != RoleRequired {
		t.Fatalf("expected unrelated required root to remain required, got %#v", got)
	}
	if got := byID["fabric/dependency-b"]; got.Role != RoleTransitive {
		t.Fatalf("expected reachable transitive dependency to remain, got %#v", got)
	}
	if got := byID["fabric/manual-jar"]; got.Role != RoleIgnored {
		t.Fatalf("expected ignored package to remain untouched when remove addresses it, got %#v", got)
	}
}

func TestPruneLockForManifestKeepsOnlyManagedClosure(t *testing.T) {
	manifest := &Manifest{Packages: []ManifestPackage{
		{ID: "fabric/root-b", Version: "compatible", Source: "auto", Role: RoleRequired, Side: SideBoth},
		{ID: "fabric/dependency-b", Version: "1.0.0", Source: "modrinth", Role: RoleTransitive, Side: SideBoth},
		{ID: "fabric/manual-jar", Version: "1.0.0", Source: "github", Role: RoleIgnored, Side: SideBoth},
	}}
	lock := &Lock{
		Version:             SupportedVersion,
		GeneratedAt:         NewLock().GeneratedAt,
		ManifestFingerprint: "sha256:test",
		GameVersion:         "1.21.5",
		Platform:            "fabric",
		PlatformVersion:     "0.16.0",
		Packages: []LockedPackage{
			{ID: "fabric/root-a", Version: "1.0.0", Source: "modrinth", URL: "https://example.com/root-a.jar", Filename: "root-a.jar", Hash: "aaa", HashAlgorithm: "sha1", InstallPath: "mods/root-a.jar", Side: "both", Provenance: []string{"root"}, Requester: "root"},
			{ID: "fabric/root-b", Version: "1.0.0", Source: "modrinth", URL: "https://example.com/root-b.jar", Filename: "root-b.jar", Hash: "bbb", HashAlgorithm: "sha1", InstallPath: "mods/root-b.jar", Side: "both", Provenance: []string{"root"}, Requester: "root"},
			{ID: "fabric/dependency-a", Version: "1.0.0", Source: "modrinth", URL: "https://example.com/dependency-a.jar", Filename: "dependency-a.jar", Hash: "ccc", HashAlgorithm: "sha1", InstallPath: "mods/dependency-a.jar", Side: "both", Provenance: []string{"root", "fabric/root-a@1.0.0"}, Requester: "fabric/root-a"},
			{ID: "fabric/dependency-b", Version: "1.0.0", Source: "modrinth", URL: "https://example.com/dependency-b.jar", Filename: "dependency-b.jar", Hash: "ddd", HashAlgorithm: "sha1", InstallPath: "mods/dependency-b.jar", Side: "both", Provenance: []string{"root", "fabric/root-b@1.0.0"}, Requester: "fabric/root-b"},
			{ID: "fabric/manual-jar", Version: "1.0.0", Source: "github", URL: "https://example.com/manual.jar", Filename: "manual.jar", Hash: "eee", HashAlgorithm: "sha1", InstallPath: "mods/manual.jar", Side: "both", Provenance: []string{"root"}, Requester: "root"},
		},
	}

	pruned := PruneLockForManifest(lock, manifest)

	if len(pruned.Packages) != 2 {
		t.Fatalf("expected 2 lock packages after pruning, got %d", len(pruned.Packages))
	}
	if pruned.Packages[0].ID != "fabric/dependency-b" || pruned.Packages[1].ID != "fabric/root-b" {
		t.Fatalf("unexpected pruned lock package set: %#v", pruned.Packages)
	}
}
