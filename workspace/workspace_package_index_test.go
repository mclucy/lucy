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
	idx.Add(makeDiscoveredPackage(t, catalog.EcosystemFabric, "sodium", "0.6.0", ""))
	idx.Add(makeDiscoveredPackage(t, catalog.EcosystemFabric, "sodium", "0.5.0", ""))
	idx.Add(makeDiscoveredPackage(t, catalog.EcosystemFabric, "lithium", "0.11.0", ""))
	pkgs := idx.Packages()
	if len(pkgs) != 4 {
		t.Fatalf("expected 4 packages, got %d", len(pkgs))
	}
	// fabric < forge by string; within fabric: lithium < sodium, then version.
	want := []struct {
		ecosystem catalog.Ecosystem
		name      string
		version   string
	}{
		{catalog.EcosystemFabric, "lithium", "0.11.0"},
		{catalog.EcosystemFabric, "sodium", "0.5.0"},
		{catalog.EcosystemFabric, "sodium", "0.6.0"},
		{catalog.EcosystemForge, "jei", "1.0.0"},
	}
	for i, expected := range want {
		if pkgs[i].ID.Ecosystem != expected.ecosystem ||
			pkgs[i].ID.Name.String() != expected.name ||
			pkgs[i].ID.Version.String() != expected.version {
			t.Errorf("wrong sort order at [%d]: %+v", i, pkgs[i].ID)
		}
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
