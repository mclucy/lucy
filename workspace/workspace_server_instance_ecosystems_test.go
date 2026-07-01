package workspace

import (
	"testing"

	"github.com/mclucy/lucy/types"
)

func TestSecondaryEcosystemsFromPackages_ConnectorOnNeoforge(t *testing.T) {
	primary := []types.Ecosystem{types.EcoNeoforge}
	pkgs := []types.DiscoveredPackage{
		{
			Id: types.VersionedPackageRef{
				PackageRef: types.PackageRef{Name: "connector"},
			},
		},
	}
	got := secondaryEcosystemsFromPackages(primary, pkgs)
	if len(got) != 1 || got[0] != types.EcoFabric {
		t.Fatalf("got %v", got)
	}
}

func TestSecondaryEcosystemsFromPackages_NoHostModLoader(t *testing.T) {
	primary := []types.Ecosystem{types.EcoFabric}
	pkgs := []types.DiscoveredPackage{
		{Id: types.VersionedPackageRef{PackageRef: types.PackageRef{Name: "connector"}}},
	}
	if got := secondaryEcosystemsFromPackages(primary, pkgs); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestServerInstance_SecondaryEcosystem_FromPackages(t *testing.T) {
	neoforgeEntry, _ := DefaultRegistry.FindEntry(types.RuntimeNodeNeoforge)
	topo := BuildTopologyFromEntry(neoforgeEntry)
	exec := serverFromTopology(topo)
	exec.Packages = []types.DiscoveredPackage{
		{Id: types.VersionedPackageRef{PackageRef: types.PackageRef{Name: "sinytra-connector"}}},
	}
	got := exec.SecondaryEcosystem()
	if len(got) != 1 || got[0] != types.EcoFabric {
		t.Fatalf("got %v", got)
	}
}
