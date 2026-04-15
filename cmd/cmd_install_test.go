package cmd

import (
	"reflect"
	"testing"

	"github.com/mclucy/lucy/state"
	"github.com/mclucy/lucy/types"
)

func TestBuildInstallSyncPlanUsesExactLockClosureWithinManagedScope(t *testing.T) {
	manifest := &state.Manifest{
		Format: state.ManifestDefaults().Format,
		Environment: state.ManifestEnvironment{
			Platform: string(types.PlatformFabric),
		},
		Layout: state.ManifestDefaults().Layout,
		Policy: state.ManifestPolicy{
			ManagedRoots:   []string{"mods"},
			UnmanagedPaths: []string{"mods/ignored/**"},
		},
		Packages: []state.ManifestPackage{
			{ID: "fabric/root", Version: "compatible", Source: "auto", Role: state.RoleRequired, Side: state.SideBoth},
			{ID: "fabric/dependency", Version: "1.0.0", Source: "modrinth", Role: state.RoleTransitive, Side: state.SideBoth},
			{ID: "fabric/manual", Version: "1.0.0", Source: "github", Role: state.RoleIgnored, Side: state.SideBoth},
		},
	}
	lock := &state.Lock{
		ManifestFingerprint: manifestFingerprint(manifest, ""),
		Packages: []state.LockedPackage{
			{ID: "fabric/root", Version: "1.2.3", InstallPath: "mods/root.jar"},
			{ID: "fabric/dependency", Version: "4.5.6", InstallPath: "mods/dependency.jar"},
			{ID: "fabric/manual", Version: "7.8.9", InstallPath: "mods/ignored/manual.jar"},
			{ID: "fabric/unmanaged", Version: "0.0.1", InstallPath: "world/datapacks/extra.jar"},
		},
	}

	plan, err := buildInstallSyncPlan(manifest, lock)
	if err != nil {
		t.Fatalf("build install sync plan: %v", err)
	}

	if !plan.UsesExactLock {
		t.Fatal("expected current lockfile facts to drive install sync")
	}

	got := packageIDsToStrings(plan.Requested)
	want := []string{"fabric/dependency@4.5.6", "fabric/root@1.2.3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected exact sync request set: got %#v want %#v", got, want)
	}
	if !plan.Stable {
		t.Fatal("expected exact lock sync plan to be marked stable")
	}
}

func TestBuildInstallSyncPlanFallsBackToRequiredIntentWhenLockIsStale(t *testing.T) {
	manifest := &state.Manifest{
		Format: state.ManifestDefaults().Format,
		Environment: state.ManifestEnvironment{
			Platform: string(types.PlatformFabric),
		},
		Layout: state.ManifestDefaults().Layout,
		Policy: state.ManifestDefaults().Policy,
		Packages: []state.ManifestPackage{
			{ID: "fabric/root", Version: "compatible", Source: "auto", Role: state.RoleRequired, Side: state.SideBoth},
			{ID: "fabric/dependency", Version: "1.0.0", Source: "modrinth", Role: state.RoleTransitive, Side: state.SideBoth},
			{ID: "fabric/manual", Version: "1.0.0", Source: "github", Role: state.RoleIgnored, Side: state.SideBoth},
		},
	}
	lock := &state.Lock{
		ManifestFingerprint: "sha256:stale",
		Packages: []state.LockedPackage{
			{ID: "fabric/root", Version: "9.9.9", InstallPath: "mods/root.jar"},
			{ID: "fabric/dependency", Version: "8.8.8", InstallPath: "mods/dependency.jar"},
		},
	}

	plan, err := buildInstallSyncPlan(manifest, lock)
	if err != nil {
		t.Fatalf("build install sync plan: %v", err)
	}

	if plan.UsesExactLock {
		t.Fatal("expected stale lockfile to force manifest-intent resolution")
	}

	got := packageIDsToStrings(plan.Requested)
	want := []string{"fabric/root@compatible"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected manifest fallback request set: got %#v want %#v", got, want)
	}
	if plan.Stable {
		t.Fatal("expected stale lock sync plan to require lock refresh")
	}
}

func packageIDsToStrings(ids []types.PackageId) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.StringFull())
	}
	return out
}
