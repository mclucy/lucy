package state

import "testing"

func TestLockToArtifactSetRetainsEmbeddedParentLinkage(t *testing.T) {
	lock := &Lock{
		Packages: []LockedPackage{
			{
				ID:            "neoforge/parent-mod",
				Version:       "1.0.0",
				Source:        "modrinth",
				Hash:          "parenthash",
				HashAlgorithm: "sha512",
				InstallPath:   "mods/parent-mod.jar",
				Side:          "server",
				Provenance:    []string{"root"},
			},
			{
				ID:            "neoforge/embedded-lib",
				Version:       "2.0.0",
				Source:        "direct",
				Hash:          "embeddedhash",
				HashAlgorithm: "sha512",
				InstallPath:   "mods/parent-mod.jar!/META-INF/jarjar/embedded-lib.jar",
				Side:          "server",
				Embedded:      true,
				EmbeddedIn:    "neoforge/parent-mod",
				Provenance:    []string{"root", "neoforge/parent-mod@1.0.0"},
			},
		},
	}

	artifacts := LockToArtifactSet(lock)
	if len(artifacts.Packages) != 2 {
		t.Fatalf("expected 2 package artifacts, got %d", len(artifacts.Packages))
	}

	embedded := EmbeddedPackages(artifacts)
	if len(embedded) != 1 {
		t.Fatalf("expected 1 embedded artifact, got %d", len(embedded))
	}
	if embedded[0].EmbeddedIn != "neoforge/parent-mod" {
		t.Fatalf("expected embedded parent linkage to be preserved, got %q", embedded[0].EmbeddedIn)
	}
	if embedded[0].Class != ClassEmbedded {
		t.Fatalf("expected embedded artifact class %q, got %q", ClassEmbedded, embedded[0].Class)
	}

	managed := ManagedPackages(artifacts)
	if len(managed) != 1 {
		t.Fatalf("expected 1 non-embedded managed artifact, got %d", len(managed))
	}
	if managed[0].ID != "neoforge/parent-mod" {
		t.Fatalf("expected parent artifact to remain a managed package, got %q", managed[0].ID)
	}
}

func TestIsManagedExcludesIgnoredPaths(t *testing.T) {
	scope := NewManagedScope([]string{"mods", "plugins", "config"}, []string{"mods/ignored/**", "world/**"})

	if !IsManaged(scope, "mods/sodium.jar") {
		t.Fatalf("expected mods/sodium.jar to be managed")
	}
	if IsManaged(scope, "mods/ignored/debug.jar") {
		t.Fatalf("expected ignored glob to exclude managed path")
	}
	if IsManaged(scope, "world/datapacks/custom.zip") {
		t.Fatalf("expected world paths to remain observed-only")
	}
}

func TestClassifyPathUsesManagedRoots(t *testing.T) {
	scope := NewManagedScope([]string{"mods", "plugins", "config"}, []string{"mods/ignored/**"})

	if got := ClassifyPath(scope, "mods/sodium.jar"); got != ClassMod {
		t.Fatalf("expected class %q, got %q", ClassMod, got)
	}
	if got := ClassifyPath(scope, "world/level.dat"); got != ClassUnmanaged {
		t.Fatalf("expected unmanaged world path, got %q", got)
	}
}
