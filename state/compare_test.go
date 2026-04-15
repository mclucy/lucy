package state

import (
	"reflect"
	"testing"
)

func TestDiffDesiredResolved(t *testing.T) {
	manifest := &Manifest{
		Packages: []ManifestPackage{
			{ID: "fabric/a", Version: "1.0.0", Source: "modrinth", Side: SideBoth},
			{ID: "fabric/b", Version: "1.0.0", Source: "modrinth", Side: SideBoth},
		},
	}
	lock := &Lock{
		Packages: []LockedPackage{
			{ID: "fabric/a", InstallPath: "mods/a.jar"},
			{ID: "fabric/c", InstallPath: "mods/c.jar"},
		},
	}

	diff := DiffDesiredResolved(manifest, lock)

	if !reflect.DeepEqual(diff.InManifestNotLock, []string{"fabric/b"}) {
		t.Fatalf("expected manifest-only package, got %#v", diff.InManifestNotLock)
	}
	if !reflect.DeepEqual(diff.InLockNotManifest, []string{"fabric/c"}) {
		t.Fatalf("expected lock-only package, got %#v", diff.InLockNotManifest)
	}
}

func TestDiffDesiredResolvedTreatsFuzzyIntentAndExactLockAsSameMembership(t *testing.T) {
	manifest := &Manifest{
		Packages: []ManifestPackage{{
			ID:      "fabric/lithium",
			Version: "compatible",
			Source:  "modrinth",
			Side:    SideBoth,
		}},
	}
	lock := &Lock{
		ManifestFingerprint: "sha256:stale-or-current",
		Packages: []LockedPackage{{
			ID:          "fabric/lithium",
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
			{ID: "fabric/a", InstallPath: "mods/a.jar"},
			{ID: "fabric/b", InstallPath: "mods/b.jar"},
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

func TestDiffResolvedObservedDistinguishesManagedDriftFromIgnoredAndUnmanagedContent(t *testing.T) {
	lock := &Lock{
		Packages: []LockedPackage{{
			ID:          "fabric/a",
			InstallPath: "mods/a.jar",
		}},
	}

	scope := NewManagedScope([]string{"mods", "plugins"}, []string{"world/**"})
	diff := DiffResolvedObservedInScope(lock, []string{
		"mods/a.jar",
		"mods/extra.jar",
		"mods/manual.jar",
		"world/level.dat",
	}, scope, []string{"mods/manual.jar"})

	if !reflect.DeepEqual(diff.InObservedNotLock, []string{"mods/extra.jar"}) {
		t.Fatalf("expected managed observed drift only, got %#v", diff.InObservedNotLock)
	}
	if !reflect.DeepEqual(diff.IgnoredObserved, []string{"mods/manual.jar"}) {
		t.Fatalf("expected ignored/manual observed content to stay visible but separate, got %#v", diff.IgnoredObserved)
	}
	if !reflect.DeepEqual(diff.UnmanagedObserved, []string{"world/level.dat"}) {
		t.Fatalf("expected unmanaged observed content to stay separate, got %#v", diff.UnmanagedObserved)
	}
}

func TestIgnoredInstallPaths(t *testing.T) {
	manifest := &Manifest{
		Packages: []ManifestPackage{
			{ID: "fabric/a", Role: RoleRequired},
			{ID: "fabric/manual", Role: RoleIgnored},
			{ID: "fabric/missing", Role: RoleIgnored},
		},
	}
	lock := &Lock{
		Packages: []LockedPackage{
			{ID: "fabric/a", InstallPath: "mods/a.jar"},
			{ID: "fabric/manual", InstallPath: "mods/manual.jar"},
		},
	}

	got := IgnoredInstallPaths(manifest, lock)
	if !reflect.DeepEqual(got, []string{"mods/manual.jar"}) {
		t.Fatalf("expected ignored install paths from manifest+lock, got %#v", got)
	}
}

func TestCompareManifestLockObservedSeparatesIntentFactAndObservedLayers(t *testing.T) {
	manifest := &Manifest{
		Policy: ManifestPolicy{ManagedRoots: []string{"mods"}, UnmanagedPaths: []string{"world/**"}},
		Packages: []ManifestPackage{
			{ID: "fabric/a", Role: RoleRequired},
			{ID: "fabric/b", Role: RoleRequired},
			{ID: "fabric/manual", Role: RoleIgnored},
		},
	}
	lock := &Lock{
		Packages: []LockedPackage{
			{ID: "fabric/a", InstallPath: "mods/a.jar"},
			{ID: "fabric/transitive", InstallPath: "mods/transitive.jar"},
			{ID: "fabric/manual", InstallPath: "mods/manual.jar"},
		},
	}

	diff := CompareManifestLockObserved(manifest, lock, []string{
		"mods/a.jar",
		"mods/manual.jar",
		"mods/extra.jar",
		"world/level.dat",
	})

	if !reflect.DeepEqual(diff.InManifestNotLock, []string{"fabric/b"}) {
		t.Fatalf("expected manifest intent drift, got %#v", diff.InManifestNotLock)
	}
	if !reflect.DeepEqual(diff.InLockNotManifest, []string{"fabric/transitive"}) {
		t.Fatalf("expected stale lock facts only for non-ignored entries, got %#v", diff.InLockNotManifest)
	}
	if !reflect.DeepEqual(diff.InLockNotObserved, []string{"mods/transitive.jar"}) {
		t.Fatalf("expected lock-vs-observed drift for managed path, got %#v", diff.InLockNotObserved)
	}
	if !reflect.DeepEqual(diff.InObservedNotLock, []string{"mods/extra.jar"}) {
		t.Fatalf("expected managed observed extra, got %#v", diff.InObservedNotLock)
	}
	if !reflect.DeepEqual(diff.IgnoredObserved, []string{"mods/manual.jar"}) {
		t.Fatalf("expected ignored observed content to stay separate, got %#v", diff.IgnoredObserved)
	}
	if !reflect.DeepEqual(diff.UnmanagedObserved, []string{"world/level.dat"}) {
		t.Fatalf("expected unmanaged observed content to stay separate, got %#v", diff.UnmanagedObserved)
	}
}

func TestClassifyDrift(t *testing.T) {
	tests := []struct {
		name string
		diff StateDiff
		want string
	}{
		{
			name: "in sync",
			want: "in sync",
		},
		{
			name: "has unresolved intent",
			diff: StateDiff{InManifestNotLock: []string{"fabric/a"}},
			want: "has unresolved intent",
		},
		{
			name: "has stale lock facts",
			diff: StateDiff{InLockNotManifest: []string{"fabric/transitive"}},
			want: "has stale lock facts",
		},
		{
			name: "has runtime drift from lock missing observed",
			diff: StateDiff{InLockNotObserved: []string{"mods/a.jar"}},
			want: "has runtime drift",
		},
		{
			name: "has runtime drift from observed extra",
			diff: StateDiff{InObservedNotLock: []string{"mods/extra.jar"}},
			want: "has runtime drift",
		},
		{
			name: "has ignored manual content",
			diff: StateDiff{IgnoredObserved: []string{"mods/manual.jar"}, UnmanagedObserved: []string{"world/level.dat"}},
			want: "has ignored/manual content",
		},
		{
			name: "has intent drift stale lock facts runtime drift and ignored content",
			diff: StateDiff{
				InManifestNotLock: []string{"fabric/a"},
				InLockNotManifest: []string{"fabric/transitive"},
				InObservedNotLock: []string{"mods/extra.jar"},
				IgnoredObserved:   []string{"mods/manual.jar"},
			},
			want: "has unresolved intent, stale lock facts, runtime drift, and ignored/manual content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyDrift(tt.diff); got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}
