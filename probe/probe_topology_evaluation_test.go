package probe

import (
	"github.com/mclucy/lucy/types"
	"testing"
)

// --- EvaluateCompatibility ---

func TestEvaluateCompatibility_NilTopology(t *testing.T) {
	result := EvaluateCompatibility(nil, types.CapabilityFabricMods)
	if result.Verdict != types.CompatUnresolved {
		t.Errorf("expected CompatUnresolved for nil topology, got %q", result.Verdict)
	}
	if result.Reason != "topology_unresolved" {
		t.Errorf("unexpected reason: %q", result.Reason)
	}
}

func TestEvaluateCompatibility_UnresolvedTopology(t *testing.T) {
	topo := &types.RuntimeTopology{} // empty = unresolved (no PrimaryNode, no Nodes)
	result := EvaluateCompatibility(topo, types.CapabilityFabricMods)
	if result.Verdict != types.CompatUnresolved {
		t.Errorf("expected CompatUnresolved for empty topology, got %q", result.Verdict)
	}
}

func TestEvaluateCompatibility_DirectCapabilityMatch(t *testing.T) {
	fabricEntry, _ := DefaultRegistry.FindEntry(RuntimeNodeFabric)
	topo := BuildTopologyFromEntry(fabricEntry)
	result := EvaluateCompatibility(topo, types.CapabilityFabricMods)
	if result.Verdict != types.CompatCompatible {
		t.Errorf("expected CompatCompatible for fabric+fabric_mods, got %q", result.Verdict)
	}
	if result.Reason != "direct_capability_match" {
		t.Errorf("unexpected reason: %q", result.Reason)
	}
	if result.RiskLevel != types.RiskNone {
		t.Errorf("expected RiskNone, got %d", result.RiskLevel)
	}
}

func TestEvaluateCompatibility_Incompatible(t *testing.T) {
	fabricEntry, _ := DefaultRegistry.FindEntry(RuntimeNodeFabric)
	topo := BuildTopologyFromEntry(fabricEntry)
	result := EvaluateCompatibility(topo, types.CapabilityForgeMods)
	if result.Verdict != types.CompatIncompatible {
		t.Errorf("expected CompatIncompatible for fabric+forge_mods, got %q", result.Verdict)
	}
	if result.Reason != "no_capability_match" {
		t.Errorf("unexpected reason: %q", result.Reason)
	}
}

func TestEvaluateCompatibility_BridgeCompatible_LowRisk(t *testing.T) {
	// Build a topology where node A bridges to node B (low risk), B has forge_mods
	nodeA := makeNode("bridge_node")
	nodeB := makeNode("forge_node", types.CapabilityForgeMods)
	edge := makeEdge("bridge_node", "forge_node", types.EdgeBridges, types.RiskLow)
	topo := makeTopology("bridge_node", []types.RuntimeNode{nodeA, nodeB}, []types.RuntimeEdge{edge})
	result := EvaluateCompatibility(topo, types.CapabilityForgeMods)
	if result.Verdict != types.CompatCompatible {
		t.Errorf("expected CompatCompatible for low-risk bridge, got %q", result.Verdict)
	}
	if result.Reason != "direct_capability_match" {
		t.Errorf("unexpected reason: %q", result.Reason)
	}
}

func TestEvaluateCompatibility_BridgeCompatible_HighRisk_Degraded(t *testing.T) {
	nodeA := makeNode("bridge_node")
	nodeB := makeNode("target_node", types.CapabilityForgeMods)
	edge := makeEdge("bridge_node", "target_node", types.EdgeBridges, types.RiskHigh)
	topo := makeTopology("bridge_node", []types.RuntimeNode{nodeA, nodeB}, []types.RuntimeEdge{edge})
	result := EvaluateCompatibility(topo, types.CapabilityForgeMods)
	if result.Verdict != types.CompatCompatible {
		t.Errorf("expected CompatCompatible (nodeB in Nodes), got %q", result.Verdict)
	}
	if result.Reason != "direct_capability_match" {
		t.Errorf("unexpected reason: %q", result.Reason)
	}
}

func TestEvaluateCompatibility_BridgeEdgeUsedForCompatibility(t *testing.T) {
	nodeA := makeNode("bridge_node")
	nodeB := makeNode("target_node", types.CapabilityFabricMods)
	edge := makeEdge("bridge_node", "target_node", types.EdgeBridges, types.RiskLow)
	topo := makeTopology("bridge_node", []types.RuntimeNode{nodeA, nodeB}, []types.RuntimeEdge{edge})
	result := EvaluateCompatibility(topo, types.CapabilityFabricMods)
	if result.Verdict != types.CompatCompatible {
		t.Errorf("expected CompatCompatible via bridge, got %q", result.Verdict)
	}
	if result.Reason != "direct_capability_match" {
		t.Errorf("unexpected reason: %q", result.Reason)
	}
}

func TestEvaluateCompatibility_BridgeEdgeHighRiskDegraded(t *testing.T) {
	nodeA := makeNode("bridge_node")
	nodeB := makeNode("target_node", types.CapabilityForgeMods)
	edge := makeEdge("bridge_node", "target_node", types.EdgeBridges, types.RiskHigh)
	topo := makeTopology("bridge_node", []types.RuntimeNode{nodeA, nodeB}, []types.RuntimeEdge{edge})
	result := EvaluateCompatibility(topo, types.CapabilityForgeMods)
	if result.Verdict != types.CompatCompatible {
		t.Errorf("expected CompatCompatible (nodeB in Nodes), got %q", result.Verdict)
	}
}

func TestEvaluateCompatibility_BridgeEdgeWrongKind_Ignored(t *testing.T) {
	nodeA := makeNode("host_node")
	nodeB := makeNode("target_node", types.CapabilityForgeMods)
	edge := makeEdge("host_node", "target_node", types.EdgeHosts, types.RiskNone)
	topo := makeTopology("host_node", []types.RuntimeNode{nodeA, nodeB}, []types.RuntimeEdge{edge})
	result := EvaluateCompatibility(topo, types.CapabilityForgeMods)
	if result.Verdict != types.CompatCompatible {
		t.Errorf("expected CompatCompatible (nodeB has capability), got %q", result.Verdict)
	}
	if result.Reason != "direct_capability_match" {
		t.Errorf("unexpected reason: %q", result.Reason)
	}
}

func TestEvaluateCompatibility_HybridNode_MultipleCapabilities(t *testing.T) {
	// Arclight has both ForgeMods and BukkitPlugins
	arclightEntry, _ := DefaultRegistry.FindEntry(RuntimeNodeArclight)
	topo := BuildTopologyFromEntry(arclightEntry)

	forgeResult := EvaluateCompatibility(topo, types.CapabilityForgeMods)
	if forgeResult.Verdict != types.CompatCompatible {
		t.Errorf("arclight should support forge_mods, got %q", forgeResult.Verdict)
	}

	bukkitResult := EvaluateCompatibility(topo, types.CapabilityBukkitPlugins)
	if bukkitResult.Verdict != types.CompatCompatible {
		t.Errorf("arclight should support bukkit_plugins, got %q", bukkitResult.Verdict)
	}
}

// --- CapabilityForPlatform ---

func TestCapabilityForPlatform_KnownPlatforms(t *testing.T) {
	cases := []struct {
		platform types.Platform
		want     types.RuntimeCapability
	}{
		{types.PlatformFabric, types.CapabilityFabricMods},
		{types.PlatformForge, types.CapabilityForgeMods},
		{types.PlatformNeoforge, types.CapabilityNeoforgeMods},
		{types.PlatformMCDR, types.CapabilityMCDRPlugins},
	}
	for _, tc := range cases {
		got := CapabilityForPlatform(tc.platform)
		if got != tc.want {
			t.Errorf("CapabilityForPlatform(%q) = %q, want %q", tc.platform, got, tc.want)
		}
	}
}

func TestCapabilityForPlatform_UnknownPlatform(t *testing.T) {
	cases := []types.Platform{
		types.PlatformMinecraft,
		types.PlatformAny,
		types.PlatformNone,
		"unknown_platform",
	}
	for _, p := range cases {
		got := CapabilityForPlatform(p)
		if got != "" {
			t.Errorf("CapabilityForPlatform(%q) = %q, want empty string", p, got)
		}
	}
}
