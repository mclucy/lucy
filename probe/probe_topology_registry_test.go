package probe

import (
	"testing"

	"github.com/mclucy/lucy/types"
)

func TestNewRuntimeRegistry_FindEntry_KnownNode(t *testing.T) {
	entry, ok := DefaultRegistry.FindEntry(types.RuntimeNodeFabric)
	if !ok {
		t.Fatal("expected to find fabric entry")
	}
	if entry.NodeID != types.RuntimeNodeFabric {
		t.Errorf("wrong node ID: %q", entry.NodeID)
	}
	if entry.Role != types.RuntimeRoleModLoader {
		t.Errorf("wrong role: %q", entry.Role)
	}
	found := false
	for _, cap := range entry.Capabilities {
		if cap == types.CapabilityFabricMods {
			found = true
		}
	}
	if !found {
		t.Error("fabric entry missing CapabilityFabricMods")
	}
}

func TestNewRuntimeRegistry_FindEntry_UnknownNode(t *testing.T) {
	_, ok := DefaultRegistry.FindEntry("nonexistent_node")
	if ok {
		t.Error("expected not found for unknown node")
	}
}

func TestNewRuntimeRegistry_FindEntry_ReturnsCopy(t *testing.T) {
	entry, _ := DefaultRegistry.FindEntry(types.RuntimeNodeFabric)
	entry.Capabilities = append(entry.Capabilities, "mutated")
	original, _ := DefaultRegistry.FindEntry(types.RuntimeNodeFabric)
	for _, cap := range original.Capabilities {
		if cap == "mutated" {
			t.Error("FindEntry returned a reference, not a copy")
		}
	}
}

func TestBuildTopologyFromEntry_SimpleNode(t *testing.T) {
	entry := RegistryEntry{
		NodeID:       types.RuntimeNodeFabric,
		Role:         types.RuntimeRoleModLoader,
		Capabilities: []types.RuntimeCapability{types.CapabilityFabricMods},
	}
	topo := BuildTopologyFromEntry(entry)
	if topo == nil {
		t.Fatal("expected non-nil topology")
	}
	if topo.PrimaryNode != types.RuntimeNodeFabric {
		t.Errorf("wrong primary node: %q", topo.PrimaryNode)
	}
	if len(topo.Nodes) != 1 {
		t.Errorf("expected 1 node, got %d", len(topo.Nodes))
	}
	if len(topo.Edges) != 0 {
		t.Errorf("expected 0 edges, got %d", len(topo.Edges))
	}
}

func TestBuildTopologyFromEntry_WithPolicyEdges(t *testing.T) {
	entry, ok := DefaultRegistry.FindEntry(types.RuntimeNodePaperFork)
	if !ok {
		t.Fatal("paper-fork not in registry")
	}
	topo := BuildTopologyFromEntry(entry)
	if topo == nil {
		t.Fatal("expected non-nil topology")
	}
	// Should have paper-fork + paper nodes
	if len(topo.Nodes) < 2 {
		t.Errorf("expected at least 2 nodes (paper-fork + paper), got %d", len(topo.Nodes))
	}
	if len(topo.Edges) != 1 {
		t.Errorf("expected 1 edge, got %d", len(topo.Edges))
	}
	edge := topo.Edges[0]
	if edge.From != types.RuntimeNodePaperFork || edge.To != types.RuntimeNodePaper || edge.Verb != types.EdgeImplements {
		t.Errorf("unexpected edge: %+v", edge)
	}
}

func TestBuildTopologyFromEntry_PaperUsesVanillaAnchor(t *testing.T) {
	entry, ok := DefaultRegistry.FindEntry(types.RuntimeNodePaper)
	if !ok {
		t.Fatal("paper not in registry")
	}

	topo := BuildTopologyFromEntry(entry)
	if topo == nil {
		t.Fatal("expected non-nil topology")
	}
	if len(topo.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(topo.Edges))
	}

	edge := topo.Edges[0]
	if edge.From != types.RuntimeNodePaper || edge.To != types.RuntimeNodeMinecraft || edge.Verb != types.EdgeModifies {
		t.Errorf("unexpected edge: %+v", edge)
	}
}

func TestNewRuntimeRegistry_PolicyEdgesUseSurvivingVerbs(t *testing.T) {
	for _, nodeID := range []types.RuntimeNodeID{
		types.RuntimeNodePaper,
		types.RuntimeNodePaperFork,
		types.RuntimeNodeSpigot,
		types.RuntimeNodeCraftBukkit,
		types.RuntimeNodeFolia,
		types.RuntimeNodeLeaves,
	} {
		entry, ok := DefaultRegistry.FindEntry(nodeID)
		if !ok {
			t.Fatalf("missing registry entry for %q", nodeID)
		}

		for _, edge := range entry.PolicyEdges {
			switch edge.Kind {
			case types.EdgeHosts, types.EdgeImplements, types.EdgeModifies, types.EdgeProxies:
			default:
				t.Fatalf("entry %q has deprecated policy edge verb %q", nodeID, edge.Kind)
			}
		}
	}
}

func TestBuildTopologyFromEntry_UnknownNodeID(t *testing.T) {
	entry := RegistryEntry{NodeID: types.RuntimeNodeUnknown}
	topo := BuildTopologyFromEntry(entry)
	if topo == nil {
		t.Fatal("expected non-nil (empty) topology for unknown node")
	}
	if len(topo.Nodes) != 0 {
		t.Errorf("expected 0 nodes for unknown entry, got %d", len(topo.Nodes))
	}
}

func TestBuildTopologyFromEntry_NodesSorted(t *testing.T) {
	entry, _ := DefaultRegistry.FindEntry(types.RuntimeNodePaperFork)
	topo := BuildTopologyFromEntry(entry)
	for i := 1; i < len(topo.Nodes); i++ {
		if string(topo.Nodes[i-1].ID) > string(topo.Nodes[i].ID) {
			t.Errorf("nodes not sorted at index %d: %q > %q", i, topo.Nodes[i-1].ID, topo.Nodes[i].ID)
		}
	}
}

func TestNewRuntimeRegistry_CustomRegistry(t *testing.T) {
	entries := []RegistryEntry{
		{
			NodeID:       "custom_node",
			Role:         types.RuntimeRoleModLoader,
			Capabilities: []types.RuntimeCapability{types.CapabilityFabricMods},
		},
	}
	reg := NewRuntimeRegistry(entries)
	entry, ok := reg.FindEntry("custom_node")
	if !ok {
		t.Fatal("expected to find custom_node")
	}
	if entry.NodeID != "custom_node" {
		t.Errorf("wrong node ID: %q", entry.NodeID)
	}
}
