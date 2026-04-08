package probe

import (
	"github.com/mclucy/lucy/types"
	"testing"
)

func TestNewRuntimeRegistry_FindEntry_KnownNode(t *testing.T) {
	entry, ok := DefaultRegistry.FindEntry(RuntimeNodeFabric)
	if !ok {
		t.Fatal("expected to find fabric entry")
	}
	if entry.NodeID != RuntimeNodeFabric {
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
	entry, _ := DefaultRegistry.FindEntry(RuntimeNodeFabric)
	entry.Capabilities = append(entry.Capabilities, "mutated")
	original, _ := DefaultRegistry.FindEntry(RuntimeNodeFabric)
	for _, cap := range original.Capabilities {
		if cap == "mutated" {
			t.Error("FindEntry returned a reference, not a copy")
		}
	}
}

func TestNewRuntimeRegistry_LookupByPlatform_Known(t *testing.T) {
	entry, ok := DefaultRegistry.LookupByPlatform(types.PlatformFabric)
	if !ok {
		t.Fatal("expected to find entry for PlatformFabric")
	}
	if entry.NodeID != RuntimeNodeFabric {
		t.Errorf("wrong node: %q", entry.NodeID)
	}
}

func TestNewRuntimeRegistry_LookupByPlatform_PlatformAnyNotIndexed(t *testing.T) {
	// PlatformAny nodes (Paper, Spigot, etc.) should NOT be in byPlatform index
	_, ok := DefaultRegistry.LookupByPlatform(types.PlatformAny)
	if ok {
		t.Error("PlatformAny should not be indexed in byPlatform")
	}
}

func TestNewRuntimeRegistry_LookupByPlatform_Unknown(t *testing.T) {
	_, ok := DefaultRegistry.LookupByPlatform("nonexistent")
	if ok {
		t.Error("expected not found for unknown platform")
	}
}

func TestBuildTopologyFromEntry_SimpleNode(t *testing.T) {
	entry := RegistryEntry{
		NodeID:       RuntimeNodeFabric,
		Role:         types.RuntimeRoleModLoader,
		Capabilities: []types.RuntimeCapability{types.CapabilityFabricMods},
	}
	topo := BuildTopologyFromEntry(entry)
	if topo == nil {
		t.Fatal("expected non-nil topology")
	}
	if topo.PrimaryNode != RuntimeNodeFabric {
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
	// Connector bridges to Forge
	entry, ok := DefaultRegistry.FindEntry(RuntimeNodeConnector)
	if !ok {
		t.Fatal("connector not in registry")
	}
	topo := BuildTopologyFromEntry(entry)
	if topo == nil {
		t.Fatal("expected non-nil topology")
	}
	// Should have connector + forge nodes
	if len(topo.Nodes) < 2 {
		t.Errorf("expected at least 2 nodes (connector + forge), got %d", len(topo.Nodes))
	}
	if len(topo.Edges) != 1 {
		t.Errorf("expected 1 edge, got %d", len(topo.Edges))
	}
	edge := topo.Edges[0]
	if edge.From != RuntimeNodeConnector || edge.To != RuntimeNodeForge || edge.Kind != types.EdgeBridges {
		t.Errorf("unexpected edge: %+v", edge)
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
	entry, _ := DefaultRegistry.FindEntry(RuntimeNodeConnector)
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
			NodeID:           "custom_node",
			Role:             types.RuntimeRoleModLoader,
			IdentityPlatform: types.PlatformFabric,
			Capabilities:     []types.RuntimeCapability{types.CapabilityFabricMods},
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
