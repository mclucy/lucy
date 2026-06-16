package probe

import (
	"testing"

	"github.com/mclucy/lucy/types"
)

func TestPackageIndex_AddFirstWriteWins(t *testing.T) {
	idx := NewPackageIndex()
	first := makePackage(
		t,
		types.PlatformFabric,
		"sodium",
		"0.5.0",
		"/mods/sodium-0.5.0.jar",
	)
	second := makePackage(
		t,
		types.PlatformFabric,
		"sodium",
		"0.5.0",
		"/mods/sodium-other.jar",
	)
	idx.Add(first)
	idx.Add(second)
	pkgs := idx.Packages()
	if len(pkgs) != 1 {
		t.Fatalf("expected 1 package, got %d", len(pkgs))
	}
	if pkgs[0].Local.Path != "/mods/sodium-0.5.0.jar" {
		t.Errorf("first-write-wins violated: got path %q", pkgs[0].Local.Path)
	}
}

func TestPackageIndex_AddLocalPathEnrichment(t *testing.T) {
	idx := NewPackageIndex()
	remote := makePackage(t, types.PlatformFabric, "sodium", "0.5.0", "")
	local := makePackage(
		t,
		types.PlatformFabric,
		"sodium",
		"0.5.0",
		"/mods/sodium-0.5.0.jar",
	)
	idx.Add(remote)
	idx.Add(local)
	pkgs := idx.Packages()
	if len(pkgs) != 1 {
		t.Fatalf("expected 1 package, got %d", len(pkgs))
	}
	if pkgs[0].Local == nil || pkgs[0].Local.Path != "/mods/sodium-0.5.0.jar" {
		t.Errorf("local-path enrichment failed: got %+v", pkgs[0].Local)
	}
}

func TestPackageIndex_AddLocalPathNotOverwrittenByRemote(t *testing.T) {
	idx := NewPackageIndex()
	local := makePackage(
		t,
		types.PlatformFabric,
		"sodium",
		"0.5.0",
		"/mods/sodium-0.5.0.jar",
	)
	remote := makePackage(t, types.PlatformFabric, "sodium", "0.5.0", "")
	idx.Add(local)
	idx.Add(remote)
	pkgs := idx.Packages()
	if pkgs[0].Local == nil || pkgs[0].Local.Path != "/mods/sodium-0.5.0.jar" {
		t.Errorf("local path was overwritten by remote: got %+v", pkgs[0].Local)
	}
}

func TestPackageIndex_PackagesSortOrder(t *testing.T) {
	idx := NewPackageIndex()
	idx.Add(makePackage(t, types.PlatformForge, "jei", "1.0.0", ""))
	idx.Add(makePackage(t, types.PlatformFabric, "sodium", "0.5.0", ""))
	idx.Add(makePackage(t, types.PlatformFabric, "lithium", "0.11.0", ""))
	pkgs := idx.Packages()
	if len(pkgs) != 3 {
		t.Fatalf("expected 3 packages, got %d", len(pkgs))
	}
	// fabric < forge by string; within fabric: lithium < sodium
	if string(pkgs[0].Id.Platform) != "fabric" || pkgs[0].Id.Name.String() != "lithium" {
		t.Errorf("wrong sort order at [0]: %+v", pkgs[0].Id)
	}
	if string(pkgs[1].Id.Platform) != "fabric" || pkgs[1].Id.Name.String() != "sodium" {
		t.Errorf("wrong sort order at [1]: %+v", pkgs[1].Id)
	}
	if string(pkgs[2].Id.Platform) != "forge" {
		t.Errorf("wrong sort order at [2]: %+v", pkgs[2].Id)
	}
}

func TestPackageIndex_MergeBulk(t *testing.T) {
	idx := NewPackageIndex()
	pkgs := []types.Package{
		makePackage(t, types.PlatformFabric, "sodium", "0.5.0", ""),
		makePackage(t, types.PlatformFabric, "lithium", "0.11.0", ""),
		makePackage(
			t,
			types.PlatformFabric,
			"sodium",
			"0.5.0",
			"/mods/sodium.jar",
		), // enrichment
	}
	idx.Merge(pkgs)
	result := idx.Packages()
	if len(result) != 2 {
		t.Fatalf("expected 2 packages after merge, got %d", len(result))
	}
}

func TestPackageIndex_LookupByID_Found(t *testing.T) {
	idx := NewPackageIndex()
	pkg := makePackage(t, types.PlatformFabric, "sodium", "0.5.0", "")
	idx.Add(pkg)
	found, ok := idx.LookupByID(pkg.Id)
	if !ok {
		t.Fatal("expected to find package by ID")
	}
	if found.Id.Name.String() != "sodium" {
		t.Errorf("unexpected package: %+v", found.Id)
	}
}

func TestPackageIndex_LookupByID_NotFound(t *testing.T) {
	idx := NewPackageIndex()
	id := types.VersionedPackageRef{
		PackageRef: types.PackageRef{
			Platform: types.PlatformFabric,
			Name:     "missing",
		},
		Version: "1.0.0",
	}
	_, ok := idx.LookupByID(id)
	if ok {
		t.Fatal("expected not found")
	}
}

func TestPackageIndex_LookupByPlatformName_MultipleVersions(t *testing.T) {
	idx := NewPackageIndex()
	idx.Add(makePackage(t, types.PlatformFabric, "sodium", "0.6.0", ""))
	idx.Add(makePackage(t, types.PlatformFabric, "sodium", "0.5.0", ""))
	idx.Add(makePackage(t, types.PlatformFabric, "lithium", "0.11.0", ""))
	results := idx.LookupByPlatformName(types.PlatformFabric, "sodium")
	if len(results) != 2 {
		t.Fatalf("expected 2 sodium versions, got %d", len(results))
	}
	// sorted by version string ascending: 0.5.0 < 0.6.0
	if results[0].Id.Version.String() != "0.5.0" {
		t.Errorf(
			"wrong version sort: got %q first",
			results[0].Id.Version.String(),
		)
	}
}

func TestPackageIndex_LookupByPlatformName_NoneFound(t *testing.T) {
	idx := NewPackageIndex()
	results := idx.LookupByPlatformName(types.PlatformFabric, "nonexistent")
	if results != nil {
		t.Errorf("expected nil for no matches, got %v", results)
	}
}

func TestPackageIndex_EmptyIndex(t *testing.T) {
	idx := NewPackageIndex()
	if pkgs := idx.Packages(); len(pkgs) != 0 {
		t.Errorf("expected empty, got %d", len(pkgs))
	}
}
