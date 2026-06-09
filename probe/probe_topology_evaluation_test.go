package probe

import (
	"testing"

	"github.com/mclucy/lucy/types"
)

// --- EvaluateCompatibility ---

func TestEvaluateCompatibility_NilTopology(t *testing.T) {
	result := EvaluateCompatibility(nil, types.CapabilityFabricMods)
	if result.Verdict != types.CompatUnresolved {
		t.Errorf(
			"expected CompatUnresolved for nil topology, got %q",
			result.Verdict,
		)
	}
	if result.Reason != "topology_unresolved" {
		t.Errorf("unexpected reason: %q", result.Reason)
	}
}

func TestEvaluateCompatibility_UnresolvedTopology(t *testing.T) {
	topo := &types.RuntimeTopology{} // empty = unresolved (no PrimaryNode, no Nodes)
	result := EvaluateCompatibility(topo, types.CapabilityFabricMods)
	if result.Verdict != types.CompatUnresolved {
		t.Errorf(
			"expected CompatUnresolved for empty topology, got %q",
			result.Verdict,
		)
	}
}

func TestEvaluateCompatibility_DirectCapabilityMatch(t *testing.T) {
	fabricEntry, _ := DefaultRegistry.FindEntry(types.RuntimeNodeFabric)
	topo := BuildTopologyFromEntry(fabricEntry)
	result := EvaluateCompatibility(topo, types.CapabilityFabricMods)
	if result.Verdict != types.CompatCompatible {
		t.Errorf(
			"expected CompatCompatible for fabric+fabric_mods, got %q",
			result.Verdict,
		)
	}
	if result.Reason != "direct_capability_match" {
		t.Errorf("unexpected reason: %q", result.Reason)
	}
}

func TestEvaluateCompatibility_Incompatible(t *testing.T) {
	fabricEntry, _ := DefaultRegistry.FindEntry(types.RuntimeNodeFabric)
	topo := BuildTopologyFromEntry(fabricEntry)
	result := EvaluateCompatibility(topo, types.CapabilityForgeMods)
	if result.Verdict != types.CompatIncompatible {
		t.Errorf(
			"expected CompatIncompatible for fabric+forge_mods, got %q",
			result.Verdict,
		)
	}
	if result.Reason != "no_capability_match" {
		t.Errorf("unexpected reason: %q", result.Reason)
	}
}

func TestEvaluateCompatibility_IndirectHostedCapabilityIsDegraded(t *testing.T) {
	host := makeNode("neoforge")
	hosted := makeNode("sinytra", types.CapabilityFabricMods)
	edge := makeEdge("neoforge", "sinytra", types.EdgeHosts)
	topo := makeTopology(
		"neoforge",
		[]types.RuntimeNode{host, hosted},
		[]types.RuntimeEdge{edge},
	)
	result := EvaluateCompatibility(topo, types.CapabilityFabricMods)
	if result.Verdict != types.CompatDegraded {
		t.Fatalf(
			"expected hosted capability to degrade compatibility, got %q",
			result.Verdict,
		)
	}
	if result.Reason != "indirect_capability_match" {
		t.Fatalf(
			"expected indirect_capability_match reason, got %q",
			result.Reason,
		)
	}
}

func TestEvaluateCompatibility_HybridNode_MultipleCapabilities(t *testing.T) {
	// Arclight has both ForgeMods and BukkitPlugins
	arclightEntry, _ := DefaultRegistry.FindEntry(types.RuntimeNodeArclight)
	topo := BuildTopologyFromEntry(arclightEntry)

	forgeResult := EvaluateCompatibility(topo, types.CapabilityForgeMods)
	if forgeResult.Verdict != types.CompatCompatible {
		t.Errorf(
			"arclight should support forge_mods, got %q",
			forgeResult.Verdict,
		)
	}

	bukkitResult := EvaluateCompatibility(topo, types.CapabilityBukkitPlugins)
	if bukkitResult.Verdict != types.CompatCompatible {
		t.Errorf(
			"arclight should support bukkit_plugins, got %q",
			bukkitResult.Verdict,
		)
	}
}

// --- CapabilityForPlatform ---

func TestCapabilityForPlatform_KnownPlatforms(t *testing.T) {
	cases := []struct {
		platform types.PlatformId
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
			t.Errorf(
				"CapabilityForPlatform(%q) = %q, want %q",
				tc.platform,
				got,
				tc.want,
			)
		}
	}
}

func TestCapabilityForPlatform_UnknownPlatform(t *testing.T) {
	cases := []types.PlatformId{
		types.PlatformMinecraft,
		types.PlatformAny,
		types.PlatformNone,
		"unknown_platform",
	}
	for _, p := range cases {
		got := CapabilityForPlatform(p)
		if got != "" {
			t.Errorf(
				"CapabilityForPlatform(%q) = %q, want empty string",
				p,
				got,
			)
		}
	}
}
