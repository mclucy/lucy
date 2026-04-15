package state

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/mclucy/lucy/types"
)

func TestManifestRoundTrip(t *testing.T) {
	manifestText := []byte(`
[format]
version = "v1"

[environment]
game_version = "1.21.1"
platform = "fabric"
platform_version = "0.16.10"

[sources]
	[[sources.custom]]
	name = "mirror"
	url = "https://example.com/index.json"
	type = "modrinth"

[layout]
mods_dir = "mods"
plugins_dir = "plugins"
config_dir = "config"

[policy]
managed_roots = ["mods", "plugins"]
unmanaged_paths = ["world/**"]

[[packages]]
id = "fabric/fabric-api"
version = "compatible"
source = "auto"
role = "required"
side = "both"
optional = false
pinned = false

[[bundles]]
name = "server-config"
type = "config"
path = "config"
source = "./defaults/config"
optional = false
`)

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
	manifestText := []byte(`
[format]
version = "v1"

[environment]
game_version = "1.21.1"
platform = "fabric"
platform_version = "0.16.10"

[layout]
mods_dir = "mods"
plugins_dir = "plugins"
config_dir = "config"

[policy]
managed_roots = ["mods", "plugins"]
unmanaged_paths = []

[[packages]]
id = "fabric/lithium"
version = "compatible"
source = "auto"
role = "required"
side = "server"
optional = false
pinned = false

[[packages]]
id = "fabric/sodium"
version = "latest"
source = "modrinth"
role = "transitive"
side = "client"
optional = true
pinned = false

[[packages]]
id = "fabric/fabric-api"
version = "0.119.2+1.21.5"
source = "github"
role = "ignored"
side = "both"
optional = false
pinned = true
`)

	var manifest Manifest
	if err := manifest.Unmarshal(manifestText); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if len(manifest.Packages) != 3 {
		t.Fatalf("expected 3 packages, got %d", len(manifest.Packages))
	}

	gotSides := []ManifestSide{
		manifest.Packages[0].Side,
		manifest.Packages[1].Side,
		manifest.Packages[2].Side,
	}
	wantSides := []ManifestSide{SideServer, SideClient, SideBoth}
	for i := range wantSides {
		if gotSides[i] != wantSides[i] {
			t.Fatalf("package %d side mismatch: got %q want %q", i, gotSides[i], wantSides[i])
		}
	}

	if !manifest.Packages[1].Optional {
		t.Fatalf("expected client package to remain optional")
	}

	if !manifest.Packages[2].Pinned {
		t.Fatalf("expected pinned flag to remain true")
	}

	if err := ValidateManifest(manifest); err != nil {
		t.Fatalf("side-aware manifest should validate: %v", err)
	}
}

func TestManifestPreservesFuzzyVersionIntentVerbatim(t *testing.T) {
	manifestText := []byte(`
[format]
version = "v1"

[environment]
game_version = "1.21.1"
platform = "fabric"
platform_version = "0.16.10"

[layout]
mods_dir = "mods"
plugins_dir = "plugins"
config_dir = "config"

[policy]
managed_roots = ["mods", "plugins"]
unmanaged_paths = []

[[packages]]
id = "fabric/lithium"
version = ">=0.12.0 <0.13.0"
source = "auto"
role = "required"
side = "server"
optional = false
pinned = false
`)

	manifest, err := ParseManifest(manifestText)
	if err != nil {
		t.Fatalf("expected fuzzy manifest version intent to parse: %v", err)
	}

	if got := manifest.Packages[0].Version; got != ">=0.12.0 <0.13.0" {
		t.Fatalf("expected manifest to preserve fuzzy selector verbatim, got %q", got)
	}

	data, err := SerializeManifest(manifest)
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
	manifestText := []byte(`
[format]
version = "v1"

[environment]
game_version = "1.21.1"
platform = "neoforge"
platform_version = "21.1.0"
compatible_platforms = ["fabric", "mcdr", "sinytra"]

[layout]
mods_dir = "mods"
plugins_dir = "plugins"
config_dir = "config"

[policy]
managed_roots = ["mods", "plugins"]
unmanaged_paths = []

[[packages]]
id = "neoforge/connector"
version = "compatible"
source = "modrinth"
role = "required"
side = "server"
optional = false
pinned = false
`)

	manifest, err := ParseManifest(manifestText)
	if err != nil {
		t.Fatalf("expected compatible platforms manifest to parse: %v", err)
	}

	if got, want := manifest.Environment.CompatiblePlatforms, []string{"fabric", "mcdr", "sinytra"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("environment compatible platforms mismatch: got %#v want %#v", got, want)
	}
	if got := manifest.Packages[0].Version; got != "compatible" {
		t.Fatalf("expected fuzzy version intent to remain unchanged, got %q", got)
	}

	data, err := SerializeManifest(manifest)
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
}

func TestManifestRejectsIncompatibleEnvironmentCompatiblePlatforms(t *testing.T) {
	manifest := ManifestDefaults()
	manifest.Environment.Platform = "fabric"
	manifest.Environment.CompatiblePlatforms = []string{"neoforge"}

	err := ValidateManifest(manifest)
	if err == nil {
		t.Fatal("expected manifest validation to reject incompatible environment compatible platforms")
	}
	if got := err.Error(); got == "" || !bytes.Contains([]byte(got), []byte("environment.compatible_platforms")) {
		t.Fatalf("expected compatible-platform validation error, got %v", err)
	}
}

func TestManifestBundlesRemainSeparateFromPackages(t *testing.T) {
	manifestText := []byte(`
[format]
version = "v1"

[environment]
game_version = "1.20.6"
platform = "none"
platform_version = ""

[layout]
mods_dir = "mods"
plugins_dir = "plugins"
config_dir = "config"

[policy]
managed_roots = ["mods", "plugins"]
unmanaged_paths = []

[[packages]]
id = "none/luckperms"
version = "latest"
source = "curseforge"
role = "required"
side = "server"
optional = false
pinned = false

[[bundles]]
name = "default-config"
type = "config"
path = "config/luckperms"
source = "./overlays/luckperms"
optional = false

[[bundles]]
name = "vanilla-datapack"
type = "datapack"
path = "world/datapacks/vanilla"
source = "./overlays/vanilla"
optional = true
`)

	var manifest Manifest
	if err := manifest.Unmarshal(manifestText); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if len(manifest.Packages) != 1 {
		t.Fatalf("expected 1 package, got %d", len(manifest.Packages))
	}
	if len(manifest.Bundles) != 2 {
		t.Fatalf("expected 2 bundles, got %d", len(manifest.Bundles))
	}

	if manifest.Packages[0].ID == manifest.Bundles[0].Name {
		t.Fatalf("package identity space should remain separate from bundle names")
	}

	if manifest.Bundles[0].Type != BundleTypeConfig {
		t.Fatalf("expected first bundle type %q, got %q", BundleTypeConfig, manifest.Bundles[0].Type)
	}
	if manifest.Bundles[1].Type != BundleTypeDatapack {
		t.Fatalf("expected second bundle type %q, got %q", BundleTypeDatapack, manifest.Bundles[1].Type)
	}

	if err := ValidateManifest(manifest); err != nil {
		t.Fatalf("bundled manifest should validate: %v", err)
	}
}

func TestManifestDefaults(t *testing.T) {
	manifest := ManifestDefaults()

	if manifest.Format.Version != "v1" {
		t.Fatalf("expected format version v1, got %q", manifest.Format.Version)
	}
	if manifest.Environment.Platform != "none" {
		t.Fatalf("expected default platform none, got %q", manifest.Environment.Platform)
	}
	if len(manifest.Environment.CompatiblePlatforms) != 0 {
		t.Fatalf("expected no compatible platforms by default, got %#v", manifest.Environment.CompatiblePlatforms)
	}
	if manifest.Layout.ModsDir != "mods" {
		t.Fatalf("expected mods_dir mods, got %q", manifest.Layout.ModsDir)
	}
	if manifest.Layout.PluginsDir != "plugins" {
		t.Fatalf("expected plugins_dir plugins, got %q", manifest.Layout.PluginsDir)
	}
	if manifest.Layout.ConfigDir != "config" {
		t.Fatalf("expected config_dir config, got %q", manifest.Layout.ConfigDir)
	}
	if len(manifest.Policy.ManagedRoots) != 2 {
		t.Fatalf("expected 2 managed roots, got %d", len(manifest.Policy.ManagedRoots))
	}
	if manifest.Policy.ManagedRoots[0] != "mods" || manifest.Policy.ManagedRoots[1] != "plugins" {
		t.Fatalf("unexpected managed roots: %#v", manifest.Policy.ManagedRoots)
	}
	if len(manifest.Sources.Custom) != 0 {
		t.Fatalf("expected no custom sources by default")
	}
	if len(manifest.Packages) != 0 || len(manifest.Bundles) != 0 {
		t.Fatalf("expected empty package and bundle declarations by default")
	}

	if err := ValidateManifest(manifest); err != nil {
		t.Fatalf("default manifest should validate: %v", err)
	}
}

func TestManifestPreservesCompatiblePlatforms(t *testing.T) {
	manifestText := []byte(`
[format]
version = "v1"

[environment]
game_version = "1.21.1"
platform = "neoforge"
platform_version = "21.1.0"
compatible_platforms = ["fabric", "mcdr", "sinytra"]

[layout]
mods_dir = "mods"
plugins_dir = "plugins"
config_dir = "config"

[policy]
managed_roots = ["mods", "plugins"]
unmanaged_paths = []
`)

	manifest, err := ParseManifest(manifestText)
	if err != nil {
		t.Fatalf("parse manifest failed: %v", err)
	}

	want := []string{"fabric", "mcdr", "sinytra"}
	if len(manifest.Environment.CompatiblePlatforms) != len(want) {
		t.Fatalf("expected %d compatible platforms, got %d", len(want), len(manifest.Environment.CompatiblePlatforms))
	}
	for i, platform := range want {
		if manifest.Environment.CompatiblePlatforms[i] != platform {
			t.Fatalf("compatible platform %d mismatch: got %q want %q", i, manifest.Environment.CompatiblePlatforms[i], platform)
		}
	}

	data, err := SerializeManifest(manifest)
	if err != nil {
		t.Fatalf("serialize manifest failed: %v", err)
	}
	if !bytes.Contains(data, []byte("compatible_platforms = [\"fabric\", \"mcdr\", \"sinytra\"]")) {
		t.Fatalf("serialized manifest missing compatible_platforms: %s", data)
	}

	reparsed, err := ParseManifest(data)
	if err != nil {
		t.Fatalf("reparse manifest failed: %v", err)
	}
	if len(reparsed.Environment.CompatiblePlatforms) != len(want) {
		t.Fatalf("expected %d compatible platforms after round trip, got %d", len(want), len(reparsed.Environment.CompatiblePlatforms))
	}
}

func TestManifestPackagesFromClassifiedPreservesRoles(t *testing.T) {
	packages := ManifestPackagesFromClassified([]ClassifiedPackage{
		{ID: "fabric/fabric-api", Version: "0.119.2+1.21.5", Source: "modrinth", Role: RoleTransitive},
		{ID: "fabric/lithium", Version: "0.12.7", Source: "modrinth", Role: RoleRequired},
		{ID: "fabric/cloth-config", Version: "15.0.140", Source: "modrinth", Role: RoleIgnored},
	})

	if len(packages) != 3 {
		t.Fatalf("expected 3 manifest packages, got %d", len(packages))
	}
	if packages[0].ID != "fabric/cloth-config" || packages[0].Role != RoleIgnored {
		t.Fatalf("expected deterministic package ordering with ignored package preserved, got %#v", packages)
	}
	if packages[1].ID != "fabric/fabric-api" || packages[1].Role != RoleTransitive {
		t.Fatalf("expected transitive package preserved, got %#v", packages)
	}
	if packages[2].ID != "fabric/lithium" || packages[2].Role != RoleRequired {
		t.Fatalf("expected required package preserved, got %#v", packages)
	}
	for _, pkg := range packages {
		if pkg.Source != "modrinth" {
			t.Fatalf("expected source modrinth for %s, got %q", pkg.ID, pkg.Source)
		}
		if pkg.Side != SideUnknown {
			t.Fatalf("expected default side unknown for %s, got %q", pkg.ID, pkg.Side)
		}
	}
}

func TestManifestRejectsImpossibleCompatiblePlatformCombination(t *testing.T) {
	manifest := ManifestDefaults()
	manifest.Environment.Platform = "fabric"
	manifest.Environment.CompatiblePlatforms = []string{"sinytra"}

	err := ValidateManifest(manifest)
	if err == nil {
		t.Fatal("expected invalid compatible platform combination to fail")
	}
	if got := err.Error(); got == "" || !bytes.Contains([]byte(got), []byte("sinytra")) {
		t.Fatalf("expected error to mention sinytra incompatibility, got %q", got)
	}
}

func TestUpdateManifestRolesForAddPromotesExplicitRequestsAndPreservesIgnored(t *testing.T) {
	manifest := &Manifest{
		Format: ManifestDefaults().Format,
		Environment: ManifestEnvironment{
			Platform: string(types.PlatformFabric),
		},
		Layout: ManifestDefaults().Layout,
		Policy: ManifestDefaults().Policy,
		Packages: []ManifestPackage{
			{ID: "fabric/kept-root", Version: "compatible", Source: "auto", Role: RoleRequired, Side: SideBoth},
			{ID: "fabric/manual-jar", Version: "1.0.0", Source: "github", Role: RoleIgnored, Side: SideBoth, Pinned: true},
		},
	}
	lock := &Lock{
		Packages: []LockedPackage{
			{ID: "fabric/kept-root", Version: "1.0.0", Source: "modrinth", URL: "https://example.com/kept-root.jar", Filename: "kept-root.jar", Hash: "abc", HashAlgorithm: "sha1", InstallPath: "mods/kept-root.jar", Side: "both", Provenance: []string{"root"}, Requester: "root"},
			{ID: "fabric/kept-dependency", Version: "1.1.0", Source: "modrinth", URL: "https://example.com/kept-dependency.jar", Filename: "kept-dependency.jar", Hash: "def", HashAlgorithm: "sha1", InstallPath: "mods/kept-dependency.jar", Side: "both", Provenance: []string{"root", "fabric/kept-root@1.0.0"}, Requester: "fabric/kept-root"},
			{ID: "fabric/new-root", Version: "2.0.0", Source: "modrinth", URL: "https://example.com/new-root.jar", Filename: "new-root.jar", Hash: "ghi", HashAlgorithm: "sha1", InstallPath: "mods/new-root.jar", Side: "both", Provenance: []string{"root"}, Requester: "root"},
			{ID: "fabric/new-dependency", Version: "2.1.0", Source: "modrinth", URL: "https://example.com/new-dependency.jar", Filename: "new-dependency.jar", Hash: "jkl", HashAlgorithm: "sha1", InstallPath: "mods/new-dependency.jar", Side: "both", Provenance: []string{"root", "fabric/new-root@2.0.0"}, Requester: "fabric/new-root"},
		},
	}

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
	if got := byID["fabric/manual-jar"]; got.Role != RoleIgnored || got.Pinned != true || got.Version != "1.0.0" {
		t.Fatalf("expected ignored package to remain untouched, got %#v", got)
	}
}

func TestUpdateManifestRolesForRemovePrunesOrphanedTransitivesAndKeepsIgnored(t *testing.T) {
	manifest := &Manifest{
		Format: ManifestDefaults().Format,
		Environment: ManifestEnvironment{
			Platform: string(types.PlatformFabric),
		},
		Layout: ManifestDefaults().Layout,
		Policy: ManifestDefaults().Policy,
		Packages: []ManifestPackage{
			{ID: "fabric/root-a", Version: "compatible", Source: "auto", Role: RoleRequired, Side: SideBoth},
			{ID: "fabric/root-b", Version: "compatible", Source: "auto", Role: RoleRequired, Side: SideBoth},
			{ID: "fabric/dependency-a", Version: "1.0.0", Source: "modrinth", Role: RoleTransitive, Side: SideBoth},
			{ID: "fabric/dependency-b", Version: "1.0.0", Source: "modrinth", Role: RoleTransitive, Side: SideBoth},
			{ID: "fabric/manual-jar", Version: "1.0.0", Source: "github", Role: RoleIgnored, Side: SideBoth},
		},
	}
	lock := &Lock{
		Packages: []LockedPackage{
			{ID: "fabric/root-a", Version: "1.0.0", Source: "modrinth", URL: "https://example.com/root-a.jar", Filename: "root-a.jar", Hash: "aaa", HashAlgorithm: "sha1", InstallPath: "mods/root-a.jar", Side: "both", Provenance: []string{"root"}, Requester: "root"},
			{ID: "fabric/root-b", Version: "1.0.0", Source: "modrinth", URL: "https://example.com/root-b.jar", Filename: "root-b.jar", Hash: "bbb", HashAlgorithm: "sha1", InstallPath: "mods/root-b.jar", Side: "both", Provenance: []string{"root"}, Requester: "root"},
			{ID: "fabric/dependency-a", Version: "1.0.0", Source: "modrinth", URL: "https://example.com/dependency-a.jar", Filename: "dependency-a.jar", Hash: "ccc", HashAlgorithm: "sha1", InstallPath: "mods/dependency-a.jar", Side: "both", Provenance: []string{"root", "fabric/root-a@1.0.0"}, Requester: "fabric/root-a"},
			{ID: "fabric/dependency-b", Version: "1.0.0", Source: "modrinth", URL: "https://example.com/dependency-b.jar", Filename: "dependency-b.jar", Hash: "ddd", HashAlgorithm: "sha1", InstallPath: "mods/dependency-b.jar", Side: "both", Provenance: []string{"root", "fabric/root-b@1.0.0"}, Requester: "fabric/root-b"},
		},
	}

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
	manifest := &Manifest{
		Packages: []ManifestPackage{
			{ID: "fabric/root-b", Version: "compatible", Source: "auto", Role: RoleRequired, Side: SideBoth},
			{ID: "fabric/dependency-b", Version: "1.0.0", Source: "modrinth", Role: RoleTransitive, Side: SideBoth},
			{ID: "fabric/manual-jar", Version: "1.0.0", Source: "github", Role: RoleIgnored, Side: SideBoth},
		},
	}
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
