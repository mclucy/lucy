package workspace

import (
	"testing"

	"github.com/mclucy/lucy/types"
)

func serverFromTopology(topo *types.ServerTopology) *ServerInstance {
	exec := &ServerInstance{topology: topo}
	SyncServerInstanceFromTopology(exec)
	return exec
}

func TestEvaluateCompatibility_NilServer(t *testing.T) {
	result := EvaluateCompatibility(nil, types.EcoFabric)
	if result.Verdict != types.CompatUnresolved {
		t.Errorf("expected CompatUnresolved for nil server, got %q", result.Verdict)
	}
}

func TestEvaluateCompatibility_UnresolvedServer(t *testing.T) {
	exec := serverFromTopology(&types.ServerTopology{})
	result := EvaluateCompatibility(exec, types.EcoFabric)
	if result.Verdict != types.CompatUnresolved {
		t.Errorf("expected CompatUnresolved for empty topology, got %q", result.Verdict)
	}
}

func TestEvaluateCompatibility_DirectEcosystemMatch(t *testing.T) {
	fabricEntry, _ := DefaultRegistry.FindEntry(types.RuntimeNodeFabric)
	exec := serverFromTopology(BuildTopologyFromEntry(fabricEntry))
	result := EvaluateCompatibility(exec, types.EcoFabric)
	if result.Verdict != types.CompatCompatible {
		t.Errorf("expected CompatCompatible, got %q", result.Verdict)
	}
}

func TestEvaluateCompatibility_Incompatible(t *testing.T) {
	fabricEntry, _ := DefaultRegistry.FindEntry(types.RuntimeNodeFabric)
	exec := serverFromTopology(BuildTopologyFromEntry(fabricEntry))
	result := EvaluateCompatibility(exec, types.EcoForge)
	if result.Verdict != types.CompatIncompatible {
		t.Errorf("expected CompatIncompatible, got %q", result.Verdict)
	}
}

func TestEvaluateCompatibility_HostedEcosystemIsDegraded(t *testing.T) {
	host := makeNode("neoforge")
	hosted := makeNode("sinytra", types.CapabilityFabricLoader)
	edge := makeEdge("neoforge", "sinytra", types.EdgeHosts)
	topo := makeTopology(
		"neoforge",
		[]types.RuntimeNode{host, hosted},
		[]types.RuntimeEdge{edge},
	)
	exec := serverFromTopology(topo)
	result := EvaluateCompatibility(exec, types.EcoFabric)
	if result.Verdict != types.CompatDegraded {
		t.Fatalf("expected hosted fabric to degrade, got %q", result.Verdict)
	}
}

func TestEvaluateCompatibility_PaperSatisfiesBukkit(t *testing.T) {
	paperEntry, _ := DefaultRegistry.FindEntry(types.RuntimeNodePaper)
	exec := serverFromTopology(BuildTopologyFromEntry(paperEntry))
	result := EvaluateCompatibility(exec, types.EcoBukkit)
	if result.Verdict != types.CompatCompatible {
		t.Errorf("paper server should satisfy bukkit packages, got %q", result.Verdict)
	}
}

func TestEvaluateCompatibility_HybridNode(t *testing.T) {
	arclightEntry, _ := DefaultRegistry.FindEntry(types.RuntimeNodeArclight)
	exec := serverFromTopology(BuildTopologyFromEntry(arclightEntry))

	if r := EvaluateCompatibility(exec, types.EcoForge); r.Verdict != types.CompatCompatible {
		t.Errorf("arclight+forge: got %q", r.Verdict)
	}
	if r := EvaluateCompatibility(exec, types.EcoBukkit); r.Verdict != types.CompatCompatible {
		t.Errorf("arclight+bukkit: got %q", r.Verdict)
	}
}
