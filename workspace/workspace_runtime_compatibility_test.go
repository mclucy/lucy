package workspace

import (
	"testing"

	"github.com/mclucy/lucy/types"
)

func compatTestServer(
	primary types.VersionedPackageRef,
	components ...types.VersionedPackageRef,
) *ServerInstance {
	return &ServerInstance{
		PrimaryRuntime: &primary,
		PrimaryPath:    "server.jar",
		RuntimeComponents: append(
			[]types.VersionedPackageRef(nil),
			components...,
		),
	}
}

func compatTestCore(
	ecosystem types.Ecosystem,
	name string,
) types.VersionedPackageRef {
	return types.VersionedPackageRef{
		PackageRef: types.PackageRef{
			Eco:  ecosystem,
			Name: types.BarePackageName(name),
		},
		Version: "1.0.0",
	}
}

func TestEvaluateRuntimeCompatibility_UnknownRuntime(t *testing.T) {
	for _, server := range []*ServerInstance{nil, {}} {
		level, _ := EvaluateRuntimeCompatibility(server, types.EcoFabric)
		if level != types.CompatUnknown {
			t.Errorf("expected unknown compatibility, got %q", level)
		}
	}
}

func TestEvaluateRuntimeCompatibility_CompatibleAndIncompatible(t *testing.T) {
	server := compatTestServer(
		compatTestCore(types.EcoFabric, "fabric"),
	)
	level, _ := EvaluateRuntimeCompatibility(server, types.EcoFabric)
	if level != types.CompatCompatible {
		t.Errorf("expected compatible Fabric support, got %q", level)
	}
	level, _ = EvaluateRuntimeCompatibility(server, types.EcoForge)
	if level != types.CompatIncompatible {
		t.Errorf("expected incompatible Forge support, got %q", level)
	}
}

func TestEvaluateRuntimeCompatibility_ConnectorIsDegraded(t *testing.T) {
	server := compatTestServer(
		compatTestCore(types.EcoNeoforge, "neoforge"),
	)
	server.Packages = []types.DiscoveredPackage{
		{
			Id: compatTestCore(
				types.EcoNeoforge,
				"sinytra-connector",
			),
		},
	}
	level, offered := EvaluateRuntimeCompatibility(server, types.EcoFabric)
	if level != types.CompatDegraded {
		t.Fatalf("expected degraded Fabric support, got %q", level)
	}
	if offered != types.EcoFabric {
		t.Fatalf("expected Fabric offered through the bridge, got %s", offered)
	}
}

func TestEvaluateRuntimeCompatibility_PaperSatisfiesBukkit(t *testing.T) {
	server := compatTestServer(
		compatTestCore(types.EcoPaper, "paper"),
	)
	level, _ := EvaluateRuntimeCompatibility(server, types.EcoBukkit)
	if level != types.CompatCompatible {
		t.Errorf("expected compatible Bukkit support, got %q", level)
	}
}

func TestEvaluateRuntimeCompatibility_HybridCoreOffersBothEcosystems(t *testing.T) {
	server := compatTestServer(
		compatTestCore(types.EcoUnspecified, "arclight"),
		compatTestCore(types.EcoNeoforge, "neoforge"),
	)
	for _, ecosystem := range []types.Ecosystem{
		types.EcoNeoforge,
		types.EcoBukkit,
	} {
		level, _ := EvaluateRuntimeCompatibility(server, ecosystem)
		if level != types.CompatCompatible {
			t.Errorf("expected compatible %s support, got %q", ecosystem, level)
		}
	}
}
