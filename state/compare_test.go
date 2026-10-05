package state

import (
	"reflect"
	"testing"
)

func TestDiffDesiredResolved(t *testing.T) {
	manifest := &Manifest{
		Packages: []ManifestPackage{
			{ID: "a", Version: "1.0.0", Source: "modrinth", Side: SideBoth},
			{ID: "b", Version: "1.0.0", Source: "modrinth", Side: SideBoth},
		},
	}
	lock := &Lock{
		Packages: []LockedPackage{
			{ID: "a", InstallPath: "mods/a.jar"},
			{ID: "c", InstallPath: "mods/c.jar"},
		},
	}

	diff := DiffDesiredResolved(manifest, lock)

	if !reflect.DeepEqual(diff.InManifestNotLock, []string{"b"}) {
		t.Fatalf("expected manifest-only package, got %#v", diff.InManifestNotLock)
	}
	if !reflect.DeepEqual(diff.InLockNotManifest, []string{"c"}) {
		t.Fatalf("expected lock-only package, got %#v", diff.InLockNotManifest)
	}
}

func TestDiffDesiredResolvedTreatsFuzzyIntentAndExactLockAsSameMembership(t *testing.T) {
	manifest := &Manifest{
		Packages: []ManifestPackage{{
			ID:      "lithium",
			Version: "stable",
			Source:  "modrinth",
			Side:    SideBoth,
		}},
	}
	lock := &Lock{
		ManifestFingerprint: "sha256:stale-or-current",
		Packages: []LockedPackage{{
			ID:          "lithium",
			Version:     "0.12.7+mc1.21.1",
			InstallPath: "mods/lithium.jar",
		}},
	}

	diff := DiffDesiredResolved(manifest, lock)

	if len(diff.InManifestNotLock) != 0 || len(diff.InLockNotManifest) != 0 {
		t.Fatalf("expected same package ID to be considered converged membership despite fuzzy manifest intent, got %#v", diff)
	}
}

func TestDiffResolvedObserved(t *testing.T) {
	lock := &Lock{
		Packages: []LockedPackage{
			{ID: "a", InstallPath: "mods/a.jar"},
			{ID: "b", InstallPath: "mods/b.jar"},
		},
	}

	diff := DiffResolvedObserved(lock, []string{"mods/a.jar"})

	if !reflect.DeepEqual(diff.InLockNotObserved, []string{"mods/b.jar"}) {
		t.Fatalf("expected missing observed path, got %#v", diff.InLockNotObserved)
	}
	if len(diff.InObservedNotLock) != 0 {
		t.Fatalf("expected no managed observed extras, got %#v", diff.InObservedNotLock)
	}
	if len(diff.IgnoredObserved) != 0 || len(diff.UnmanagedObserved) != 0 {
		t.Fatalf("expected no ignored or unmanaged extras, got %#v", diff)
	}
}

func TestDiffResolvedObservedDistinguishesRuntimeDriftFromIgnoredContent(t *testing.T) {
	lock := &Lock{
		Packages: []LockedPackage{{
			ID:          "a",
			InstallPath: "mods/a.jar",
		}},
	}

	diff := DiffResolvedObservedInScope(lock, []string{
		"mods/a.jar",
		"mods/extra.jar",
		"mods/manual.jar",
		"world/level.dat",
	}, []string{"mods/manual.jar"})

	if !reflect.DeepEqual(diff.InObservedNotLock, []string{"mods/extra.jar", "world/level.dat"}) {
		t.Fatalf("expected managed observed drift only, got %#v", diff.InObservedNotLock)
	}
	if !reflect.DeepEqual(diff.IgnoredObserved, []string{"mods/manual.jar"}) {
		t.Fatalf("expected ignored/manual observed content to stay visible but separate, got %#v", diff.IgnoredObserved)
	}
	if len(diff.UnmanagedObserved) != 0 {
		t.Fatalf("expected scope-less comparison to produce no unmanaged content, got %#v", diff.UnmanagedObserved)
	}
}

func TestIgnoredInstallPaths(t *testing.T) {
	manifest := &Manifest{
		Packages: []ManifestPackage{
			{ID: "a", Role: RoleRequired},
			{ID: "manual", Role: RoleIgnored},
			{ID: "missing", Role: RoleIgnored},
		},
	}
	lock := &Lock{
		Packages: []LockedPackage{
			{ID: "a", InstallPath: "mods/a.jar"},
			{ID: "manual", InstallPath: "mods/manual.jar"},
		},
	}

	got := IgnoredInstallPaths(manifest, lock)
	if !reflect.DeepEqual(got, []string{"mods/manual.jar"}) {
		t.Fatalf("expected ignored install paths from manifest+lock, got %#v", got)
	}
}

func TestCompareManifestLockObservedSeparatesIntentFactAndObservedLayers(t *testing.T) {
	manifest := &Manifest{
		Packages: []ManifestPackage{
			{ID: "a", Role: RoleRequired},
			{ID: "b", Role: RoleRequired},
			{ID: "manual", Role: RoleIgnored},
		},
	}
	lock := &Lock{
		Packages: []LockedPackage{
			{ID: "a", InstallPath: "mods/a.jar"},
			{ID: "transitive", InstallPath: "mods/transitive.jar"},
			{ID: "manual", InstallPath: "mods/manual.jar"},
		},
	}

	diff := CompareManifestLockObserved(manifest, lock, []string{
		"mods/a.jar",
		"mods/manual.jar",
		"mods/extra.jar",
		"world/level.dat",
	})

	if !reflect.DeepEqual(diff.InManifestNotLock, []string{"b"}) {
		t.Fatalf("expected manifest intent drift, got %#v", diff.InManifestNotLock)
	}
	if !reflect.DeepEqual(diff.InLockNotManifest, []string{"transitive"}) {
		t.Fatalf("expected stale lock facts only for non-ignored entries, got %#v", diff.InLockNotManifest)
	}
	if !reflect.DeepEqual(diff.InLockNotObserved, []string{"mods/transitive.jar"}) {
		t.Fatalf("expected lock-vs-observed drift for managed path, got %#v", diff.InLockNotObserved)
	}
	if !reflect.DeepEqual(diff.InObservedNotLock, []string{"mods/extra.jar", "world/level.dat"}) {
		t.Fatalf("expected managed observed extra, got %#v", diff.InObservedNotLock)
	}
	if !reflect.DeepEqual(diff.IgnoredObserved, []string{"mods/manual.jar"}) {
		t.Fatalf("expected ignored observed content to stay separate, got %#v", diff.IgnoredObserved)
	}
	if len(diff.UnmanagedObserved) != 0 {
		t.Fatalf("expected scope-less comparison to produce no unmanaged content, got %#v", diff.UnmanagedObserved)
	}
}
