package workspace

import (
	"testing"

	"github.com/mclucy/lucy/types"
)

func admissionTestServer(
	primary types.VersionedPackageRef,
	components ...types.VersionedPackageRef,
) *ServerInstance {
	return &ServerInstance{
		PrimaryRuntime: &RuntimeArtifact{
			Identity: primary,
			Path:     "server.jar",
		},
		RuntimeComponents: append(
			[]types.VersionedPackageRef(nil),
			components...,
		),
	}
}

func admissionTestCore(
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

func TestEvaluateAdmission_UnresolvedRuntime(t *testing.T) {
	for _, server := range []*ServerInstance{nil, {}} {
		result := EvaluateAdmission(server, types.EcoFabric)
		if result.Verdict != AdmissionUnresolved {
			t.Errorf("expected unresolved admission, got %d", result.Verdict)
		}
	}
}

func TestEvaluateAdmission_DirectAndRejected(t *testing.T) {
	server := admissionTestServer(
		admissionTestCore(types.EcoFabric, "fabric"),
	)
	if got := EvaluateAdmission(server, types.EcoFabric); got.Verdict != AdmissionDirect {
		t.Errorf("expected direct Fabric admission, got %d", got.Verdict)
	}
	if got := EvaluateAdmission(server, types.EcoForge); got.Verdict != AdmissionRejected {
		t.Errorf("expected Forge rejection, got %d", got.Verdict)
	}
}

func TestEvaluateAdmission_ConnectorIsDegraded(t *testing.T) {
	server := admissionTestServer(
		admissionTestCore(types.EcoNeoforge, "neoforge"),
	)
	server.Packages = []types.DiscoveredPackage{
		{
			Id: admissionTestCore(
				types.EcoNeoforge,
				"sinytra-connector",
			),
		},
	}
	result := EvaluateAdmission(server, types.EcoFabric)
	if result.Verdict != AdmissionDegraded {
		t.Fatalf("expected degraded Fabric admission, got %d", result.Verdict)
	}
}

func TestEvaluateAdmission_PaperSatisfiesBukkit(t *testing.T) {
	server := admissionTestServer(
		admissionTestCore(types.EcoPaper, "paper"),
	)
	result := EvaluateAdmission(server, types.EcoBukkit)
	if result.Verdict != AdmissionDirect {
		t.Errorf("expected direct Bukkit admission, got %d", result.Verdict)
	}
}

func TestEvaluateAdmission_HybridCoreOffersBothEcosystems(t *testing.T) {
	server := admissionTestServer(
		admissionTestCore(types.EcoUnspecified, "arclight"),
		admissionTestCore(types.EcoNeoforge, "neoforge"),
	)
	for _, ecosystem := range []types.Ecosystem{
		types.EcoNeoforge,
		types.EcoBukkit,
	} {
		if got := EvaluateAdmission(server, ecosystem); got.Verdict != AdmissionDirect {
			t.Errorf("expected direct %s admission, got %d", ecosystem, got.Verdict)
		}
	}
}
