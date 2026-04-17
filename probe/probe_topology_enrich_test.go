package probe

import (
	"testing"

	"github.com/mclucy/lucy/types"
)

func TestNormalizeTopology_DeduplicatesNodes(t *testing.T) {
	nodeA := makeNode("a", types.CapabilityFabricMods)
	nodeA2 := makeNode(
		"a",
		types.CapabilityForgeMods,
	) // duplicate ID, different caps
	topo := makeTopology("a", []types.RuntimeNode{nodeA, nodeA2}, nil)
	NormalizeTopology(topo)
	if len(topo.Nodes) != 1 {
		t.Errorf("expected 1 node after dedup, got %d", len(topo.Nodes))
	}
	// last-write-wins: nodeA2 should survive
	if !topo.Nodes[0].HasCapability(types.CapabilityForgeMods) {
		t.Error("last-write-wins violated: expected nodeA2 (ForgeMods) to survive")
	}
}

func TestNormalizeTopology_DeduplicatesEdges(t *testing.T) {
	e := makeEdge("a", "b", types.EdgeHosts)
	topo := makeTopology(
		"a",
		[]types.RuntimeNode{makeNode("a"), makeNode("b")},
		[]types.RuntimeEdge{e, e}, // duplicate
	)
	NormalizeTopology(topo)
	if len(topo.Edges) != 1 {
		t.Errorf("expected 1 edge after dedup, got %d", len(topo.Edges))
	}
}

func TestNormalizeTopology_SortsNodes(t *testing.T) {
	topo := makeTopology(
		"a", []types.RuntimeNode{
			makeNode("z"),
			makeNode("a"),
			makeNode("m"),
		}, nil,
	)
	NormalizeTopology(topo)
	ids := []string{
		string(topo.Nodes[0].ID), string(topo.Nodes[1].ID),
		string(topo.Nodes[2].ID),
	}
	if ids[0] != "a" || ids[1] != "m" || ids[2] != "z" {
		t.Errorf("nodes not sorted: %v", ids)
	}
}

func TestNormalizeTopology_SortsEdges(t *testing.T) {
	topo := makeTopology(
		"a",
		[]types.RuntimeNode{makeNode("a"), makeNode("b"), makeNode("c")},
		[]types.RuntimeEdge{
			makeEdge("b", "c", types.EdgeHosts),
			makeEdge("a", "c", types.EdgeHosts),
			makeEdge("a", "b", types.EdgeHosts),
		},
	)
	NormalizeTopology(topo)
	if topo.Edges[0].From != "a" || topo.Edges[0].To != "b" {
		t.Errorf("edges not sorted correctly: first edge = %+v", topo.Edges[0])
	}
}

func TestNormalizeTopology_NilSafe(t *testing.T) {
	// Should not panic
	NormalizeTopology(nil)
}

func TestFoldTopologyRisk_PropagatesMaximumRiskAcrossConnectedNodes(t *testing.T) {
	topo := makeTopology(
		"a",
		[]types.RuntimeNode{
			{ID: "a", RiskLevel: types.RiskNone},
			{ID: "b", RiskLevel: types.RiskHigh},
		},
		[]types.RuntimeEdge{makeEdge("a", "b", types.EdgeHosts)},
	)

	FoldTopologyRisk(topo)

	for _, node := range topo.Nodes {
		if node.RiskLevel != types.RiskHigh {
			t.Fatalf("expected node %q to fold to RiskHigh, got %v", node.ID, node.RiskLevel)
		}
	}
}

func TestFoldTopologyRisk_NilSafe(t *testing.T) {
	FoldTopologyRisk(nil)
}

func TestFoldTopologyRisk_NoEdges(t *testing.T) {
	topo := makeTopology(
		"solo",
		[]types.RuntimeNode{{ID: "solo", RiskLevel: types.RiskMedium}},
		nil,
	)

	FoldTopologyRisk(topo)

	if topo.Nodes[0].RiskLevel != types.RiskMedium {
		t.Fatalf("expected isolated node risk to remain RiskMedium, got %v", topo.Nodes[0].RiskLevel)
	}
}

func TestFoldTopologyRisk_TransitivePropagation(t *testing.T) {
	topo := makeTopology(
		"a",
		[]types.RuntimeNode{
			{ID: "a", RiskLevel: types.RiskNone},
			{ID: "b", RiskLevel: types.RiskNone},
			{ID: "c", RiskLevel: types.RiskHigh},
		},
		[]types.RuntimeEdge{
			makeEdge("a", "b", types.EdgeHosts),
			makeEdge("b", "c", types.EdgeHosts),
		},
	)

	FoldTopologyRisk(topo)

	for _, node := range topo.Nodes {
		if node.RiskLevel != types.RiskHigh {
			t.Fatalf("expected transitive fold to set node %q to RiskHigh, got %v", node.ID, node.RiskLevel)
		}
	}
}

func TestMergeTopology_AddsNewNodes(t *testing.T) {
	dst := makeTopology("a", []types.RuntimeNode{makeNode("a")}, nil)
	src := makeTopology("b", []types.RuntimeNode{makeNode("b")}, nil)
	mergeTopology(dst, src)
	if len(dst.Nodes) != 2 {
		t.Errorf("expected 2 nodes after merge, got %d", len(dst.Nodes))
	}
}

func TestMergeTopology_SkipsDuplicateNodes(t *testing.T) {
	dst := makeTopology("a", []types.RuntimeNode{makeNode("a")}, nil)
	src := makeTopology("a", []types.RuntimeNode{makeNode("a")}, nil)
	mergeTopology(dst, src)
	if len(dst.Nodes) != 1 {
		t.Errorf("expected 1 node (no dup), got %d", len(dst.Nodes))
	}
}

func TestMergeTopology_AddsNewEdges(t *testing.T) {
	dst := makeTopology(
		"a",
		[]types.RuntimeNode{makeNode("a"), makeNode("b")},
		nil,
	)
	src := makeTopology(
		"a",
		[]types.RuntimeNode{makeNode("a"), makeNode("b")},
		[]types.RuntimeEdge{makeEdge("a", "b", types.EdgeHosts)},
	)
	mergeTopology(dst, src)
	if len(dst.Edges) != 1 {
		t.Errorf("expected 1 edge after merge, got %d", len(dst.Edges))
	}
}

func TestMergeTopology_SkipsDuplicateEdges(t *testing.T) {
	e := makeEdge("a", "b", types.EdgeHosts)
	dst := makeTopology(
		"a",
		[]types.RuntimeNode{makeNode("a"), makeNode("b")},
		[]types.RuntimeEdge{e},
	)
	src := makeTopology(
		"a",
		[]types.RuntimeNode{makeNode("a"), makeNode("b")},
		[]types.RuntimeEdge{e},
	)
	mergeTopology(dst, src)
	if len(dst.Edges) != 1 {
		t.Errorf("expected 1 edge (no dup), got %d", len(dst.Edges))
	}
}

func TestMergeTopology_NilSafe(t *testing.T) {
	dst := makeTopology("a", []types.RuntimeNode{makeNode("a")}, nil)
	mergeTopology(dst, nil)
	mergeTopology(nil, dst)
	// no panic
}

func TestEnrichTopologyFromPackages_NilExec(t *testing.T) {
	// Should not panic
	EnrichTopologyFromPackages(nil, nil)
}

func TestEnrichTopologyFromPackages_NoTopologyNoEvidence(t *testing.T) {
	exec := &types.RuntimeInfo{}
	EnrichTopologyFromPackages(exec, nil)
	if exec.Topology == nil {
		t.Error("expected empty topology to be set, got nil")
	}
}

func TestEnrichTopologyFromPackages_NoTopologyWithConnectorEvidence(t *testing.T) {
	exec := &types.RuntimeInfo{}
	pkgs := []types.Package{
		makePackage(t, types.PlatformFabric, "sinytra-connector", "1.0.0", ""),
	}
	EnrichTopologyFromPackages(exec, pkgs)
	if exec.Topology == nil {
		t.Fatal("expected topology to be built from evidence")
	}
	if exec.Topology.PrimaryNode != RuntimeNodeConnector {
		t.Fatalf("expected connector to be primary node, got %q", exec.Topology.PrimaryNode)
	}
	_, hasConnector := exec.Topology.FindNode(RuntimeNodeConnector)
	if !hasConnector {
		t.Error("expected connector node in topology")
	}
	_, hasFabric := exec.Topology.FindNode(RuntimeNodeFabric)
	if hasFabric {
		t.Error("did not expect fabric node in topology without host evidence")
	}
}

func TestEnrichTopologyFromPackages_NoTopologyWithKiltEvidence(t *testing.T) {
	exec := &types.RuntimeInfo{}
	pkgs := []types.Package{
		makePackage(t, types.PlatformFabric, "kilt", "1.0.0", ""),
	}
	EnrichTopologyFromPackages(exec, pkgs)
	if exec.Topology == nil {
		t.Fatal("expected topology to be built")
	}
	_, hasKilt := exec.Topology.FindNode(RuntimeNodeKilt)
	if !hasKilt {
		t.Error("expected kilt node in topology")
	}
	if exec.Topology.PrimaryNode != RuntimeNodeFabric {
		t.Fatalf("expected fabric to remain primary node, got %q", exec.Topology.PrimaryNode)
	}
	_, hasFabric := exec.Topology.FindNode(RuntimeNodeFabric)
	if !hasFabric {
		t.Error("expected fabric node in topology")
	}
	_, hasForge := exec.Topology.FindNode(RuntimeNodeForge)
	if hasForge {
		t.Error("did not expect forge node in topology without connection registry bridges")
	}
}

func TestEnrichTopologyFromPackages_ExistingTopologyEnriched(t *testing.T) {
	// Start with a fabric topology, enrich with attached geyser evidence
	fabricEntry, _ := DefaultRegistry.FindEntry(RuntimeNodeFabric)
	exec := &types.RuntimeInfo{
		Topology: BuildTopologyFromEntry(fabricEntry),
	}
	pkgs := []types.Package{
		makePackage(t, types.PlatformFabric, "geyser-fabric", "2.0.0", ""),
	}
	EnrichTopologyFromPackages(exec, pkgs)
	_, hasGeyser := exec.Topology.FindNode(RuntimeNodeGeyser)
	if !hasGeyser {
		t.Error("expected geyser node to be merged into existing topology")
	}
	if _, hasStandalone := exec.Topology.FindNode(RuntimeNodeGeyserStandalone); hasStandalone {
		t.Error("did not expect standalone geyser node from attached package evidence")
	}
}

func TestEnrichTopologyFromPackages_NoTopologyWithStandaloneGeyserHint(t *testing.T) {
	exec := &types.RuntimeInfo{
		BridgeHints: []string{string(RuntimeNodeGeyserStandalone)},
	}

	EnrichTopologyFromPackages(exec, nil)

	if exec.Topology == nil {
		t.Fatal("expected topology to be built from standalone geyser hint")
	}
	if exec.Topology.PrimaryNode != RuntimeNodeGeyserStandalone {
		t.Fatalf("expected standalone geyser to be primary node, got %q", exec.Topology.PrimaryNode)
	}
	if _, hasStandalone := exec.Topology.FindNode(RuntimeNodeGeyserStandalone); !hasStandalone {
		t.Error("expected standalone geyser node in topology")
	}
	if _, hasAttached := exec.Topology.FindNode(RuntimeNodeGeyser); hasAttached {
		t.Error("did not expect attached geyser node from standalone hint")
	}
}

func TestEnrichTopologyFromPackages_BridgeHintsProcessed(t *testing.T) {
	fabricEntry, _ := DefaultRegistry.FindEntry(RuntimeNodeFabric)
	exec := &types.RuntimeInfo{
		Topology:    BuildTopologyFromEntry(fabricEntry),
		BridgeHints: []string{string(RuntimeNodeConnector)},
	}
	EnrichTopologyFromPackages(exec, nil)
	_, hasConnector := exec.Topology.FindNode(RuntimeNodeConnector)
	if !hasConnector {
		t.Error("expected connector node from BridgeHints")
	}
}

func TestEnrichTopologyFromPackages_CaseInsensitiveNameMatching(t *testing.T) {
	exec := &types.RuntimeInfo{}
	pkgs := []types.Package{
		makePackage(t, types.PlatformFabric, "Velocity", "3.0.0", ""),
	}
	EnrichTopologyFromPackages(exec, pkgs)
	if exec.Topology == nil {
		t.Fatal("expected topology")
	}
	_, hasVelocity := exec.Topology.FindNode(RuntimeNodeVelocity)
	if !hasVelocity {
		t.Error("expected velocity node (case-insensitive name match)")
	}
}

func TestEnrichTopologyFromPackages_TopologyNormalizedAfterEnrich(t *testing.T) {
	// Add duplicate evidence to verify NormalizeTopology is called
	exec := &types.RuntimeInfo{}
	pkgs := []types.Package{
		makePackage(t, types.PlatformFabric, "sinytra-connector", "1.0.0", ""),
		makePackage(t, types.PlatformFabric, "kilt", "1.0.0", ""),
	}
	EnrichTopologyFromPackages(exec, pkgs)
	// Verify no duplicate nodes
	seen := map[types.RuntimeNodeID]int{}
	for _, n := range exec.Topology.Nodes {
		seen[n.ID]++
		if seen[n.ID] > 1 {
			t.Errorf("duplicate node %q after enrich", n.ID)
		}
	}
}
