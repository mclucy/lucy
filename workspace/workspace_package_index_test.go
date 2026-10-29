package workspace

import (
	"testing"

	"github.com/mclucy/lucy/catalog"
)

func TestPackageIndex_AddFirstWriteWins(t *testing.T) {
	idx := NewPackageIndex()
	first := makeDiscoveredPackage(
		t,
		catalog.EcosystemFabric,
		"sodium",
		"0.5.0",
		"/mods/sodium-0.5.0.jar",
	)
	second := makeDiscoveredPackage(
		t,
		catalog.EcosystemFabric,
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
	if pkgs[0].Path != "/mods/sodium-0.5.0.jar" {
		t.Errorf("first-write-wins violated: got path %q", pkgs[0].Path)
	}
}

func TestPackageIndex_AddLocalPathEnrichment(t *testing.T) {
	idx := NewPackageIndex()
	remote := makeDiscoveredPackage(t, catalog.EcosystemFabric, "sodium", "0.5.0", "")
	local := makeDiscoveredPackage(
		t,
		catalog.EcosystemFabric,
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
	if pkgs[0].Path != "/mods/sodium-0.5.0.jar" {
		t.Errorf("local-path enrichment failed: got path %q", pkgs[0].Path)
	}
}

func TestPackageIndexKeepsLocalPath(t *testing.T) {
	idx := NewPackageIndex()
	local := makeDiscoveredPackage(
		t,
		catalog.EcosystemFabric,
		"sodium",
		"0.5.0",
		"/mods/sodium-0.5.0.jar",
	)
	remote := makeDiscoveredPackage(t, catalog.EcosystemFabric, "sodium", "0.5.0", "")
	idx.Add(local)
	idx.Add(remote)
	pkgs := idx.Packages()
	if pkgs[0].Path != "/mods/sodium-0.5.0.jar" {
		t.Errorf(
			"local path was overwritten by remote: got path %q",
			pkgs[0].Path,
		)
	}
}

func TestPackageIndex_PackagesSortOrder(t *testing.T) {
	idx := NewPackageIndex()
	idx.Add(makeDiscoveredPackage(t, catalog.EcosystemForge, "jei", "1.0.0", ""))
	idx.Add(makeDiscoveredPackage(t, catalog.EcosystemFabric, "sodium", "0.5.0", ""))
	idx.Add(makeDiscoveredPackage(t, catalog.EcosystemFabric, "lithium", "0.11.0", ""))
	pkgs := idx.Packages()
	if len(pkgs) != 3 {
		t.Fatalf("expected 3 packages, got %d", len(pkgs))
	}
	// fabric < forge by string; within fabric: lithium < sodium
	if string(pkgs[0].ID.Ecosystem) != "fabric" || pkgs[0].ID.Name.String() != "lithium" {
		t.Errorf("wrong sort order at [0]: %+v", pkgs[0].ID)
	}
	if string(pkgs[1].ID.Ecosystem) != "fabric" || pkgs[1].ID.Name.String() != "sodium" {
		t.Errorf("wrong sort order at [1]: %+v", pkgs[1].ID)
	}
	if string(pkgs[2].ID.Ecosystem) != "forge" {
		t.Errorf("wrong sort order at [2]: %+v", pkgs[2].ID)
	}
}

func TestPackageIndex_MergeBulk(t *testing.T) {
	idx := NewPackageIndex()
	pkgs := []DiscoveredPackage{
		makeDiscoveredPackage(t, catalog.EcosystemFabric, "sodium", "0.5.0", ""),
		makeDiscoveredPackage(t, catalog.EcosystemFabric, "lithium", "0.11.0", ""),
		makeDiscoveredPackage(
			t,
			catalog.EcosystemFabric,
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
	pkg := makeDiscoveredPackage(t, catalog.EcosystemFabric, "sodium", "0.5.0", "")
	idx.Add(pkg)
	found, ok := idx.LookupByID(pkg.ID)
	if !ok {
		t.Fatal("expected to find package by ID")
	}
	if found.ID.Name.String() != "sodium" {
		t.Errorf("unexpected package: %+v", found.ID)
	}
}

func TestPackageIndex_LookupByID_NotFound(t *testing.T) {
	idx := NewPackageIndex()
	id := catalog.VersionedRef{
		Ecosystem: catalog.EcosystemFabric,
		Name:      "missing",
		Version:   "1.0.0",
	}
	_, ok := idx.LookupByID(id)
	if ok {
		t.Fatal("expected not found")
	}
}

func TestPackageIndexFindsAllVersions(t *testing.T) {
	idx := NewPackageIndex()
	idx.Add(makeDiscoveredPackage(t, catalog.EcosystemFabric, "sodium", "0.6.0", ""))
	idx.Add(makeDiscoveredPackage(t, catalog.EcosystemFabric, "sodium", "0.5.0", ""))
	idx.Add(makeDiscoveredPackage(t, catalog.EcosystemFabric, "lithium", "0.11.0", ""))
	results := idx.LookupByEcosystemName(catalog.EcosystemFabric, "sodium")
	if len(results) != 2 {
		t.Fatalf("expected 2 sodium versions, got %d", len(results))
	}
	// sorted by version string ascending: 0.5.0 < 0.6.0
	if results[0].ID.Version.String() != "0.5.0" {
		t.Errorf(
			"wrong version sort: got %q first",
			results[0].ID.Version.String(),
		)
	}
}
