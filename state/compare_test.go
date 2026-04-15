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
		t.Fatalf("expected no unmanaged observed paths, got %#v", diff.InObservedNotLock)
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
			name: "has drift from lock missing observed",
			diff: StateDiff{InLockNotObserved: []string{"mods/a.jar"}},
			want: "has drift",
		},
		{
			name: "has drift from observed extra",
			diff: StateDiff{InObservedNotLock: []string{"mods/extra.jar"}},
			want: "has drift",
		},
		{
			name: "has both",
			diff: StateDiff{
				InManifestNotLock: []string{"fabric/a"},
				InObservedNotLock: []string{"mods/extra.jar"},
			},
			want: "has both",
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
