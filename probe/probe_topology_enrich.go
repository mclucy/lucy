package probe

import (
	"sort"
	"strings"

	internaltopology "github.com/mclucy/lucy/probe/internal/topology"
	"github.com/mclucy/lucy/types"
)

// =============================================================================
// EVIDENCE PRECEDENCE POLICY
//
// This is the single authoritative definition of how conflicting or coexisting
// detection evidence is resolved. All conflict-resolution logic must trace back
// to this block. Never add ad-hoc precedence rules elsewhere.
//
// ECOSYSTEM FAMILIES (mutually exclusive for a single JAR)
//
//   Tier 1 – Authoritative proxy descriptors:
//     velocity        → velocity-plugin.json (Velocity-specific)
//     bungeecord      → bungee.yml           (BungeeCord-specific)
//
//   Tier 1 – Authoritative server/plugin descriptors:
//     bukkit          → plugin.yml           (Paper-family generic)
//     paper           → paper-plugin.yml     (Paper-modern specific)
//     leaves          → leaves-plugin.json   (Leaves-specific)
//     folia           → plugin.yml + folia-supported:true
//
//   Tier 1 – Authoritative sponge descriptor:
//     sponge          → META-INF/sponge_plugins.json
//
//   Tier 2 – Generic (no ecosystem proof without Tier 1):
//     plugin.yml alone → bukkit family only; never implies proxy membership
//
// CONFLICT RULE
//   If Tier-1 signals from two DIFFERENT incompatible ecosystem families are
//   detected in the same JAR (e.g., velocity-plugin.json AND bungee.yml, or
//   bungee.yml AND plugin.yml), the result for that JAR is unresolved/empty.
//   We never guess which ecosystem wins. Unresolved is always safer than wrong.
//
// INTRA-FAMILY NOTE
//   Within the Paper-family detector, descriptor precedence is already handled
//   by leaves-plugin.json > paper-plugin.yml > plugin.yml (early return wins).
//   That is NOT a conflict – it is expected descriptor layering within one JAR.
//
// IMPLEMENTATION
//   The conflict check is applied in detector_aggregator.go::Packages(), which
//   is the single point where all jar-detector results are merged. If the merged
//   platform set spans two incompatible ecosystem families, Packages() returns
//   nil, causing the caller to treat the JAR as having no recognized packages.
// =============================================================================

func EnrichTopologyFromPackages(
	exec *types.RuntimeInfo,
	packages []types.Package,
) {
	if exec == nil {
		return
	}

	evidence := detectedRuntimeEvidence(packages)
	evidence = append(evidence, detectedRuntimeEvidenceFromHints(exec.BridgeHints)...)

	if exec.Topology == nil {
		// No topology yet — attempt to build one from package evidence.
		if len(evidence) == 0 {
			exec.Topology = &types.RuntimeTopology{}
			return
		}

		if inferred := inferHostTopologyFromAttachedBridgePackages(packages); inferred != nil {
			exec.Topology = inferred
		} else {

			// Build from the first evidence node, merge the rest.
			firstEntry, ok := FindEntry(evidence[0])
			if !ok {
				exec.Topology = &types.RuntimeTopology{}
				return
			}
			exec.Topology = BuildTopologyFromEntry(firstEntry)
			if exec.Topology == nil {
				exec.Topology = &types.RuntimeTopology{}
				return
			}
		}

		for _, nodeID := range evidence {
			entry, ok := FindEntry(nodeID)
			if !ok {
				continue
			}
			annotation := BuildTopologyFromEntry(entry)
			if annotation == nil {
				continue
			}
			mergeTopology(exec.Topology, annotation)
		}

		applyDeclarativeConnections(exec.Topology, internaltopology.DefaultConnectionRegistry)
		NormalizeTopology(exec.Topology)
		FoldTopologyRisk(exec.Topology)
		return
	}

	// Topology exists (resolved or not) — enrich with package evidence.
	// This is additive annotation: bridge/adaptor evidence like Connector augments
	// the existing host runtime topology without replacing the current primary
	// runtime identity (for example, NeoForge remains the primary node).
	for _, nodeID := range evidence {
		entry, ok := FindEntry(nodeID)
		if !ok {
			continue
		}

		annotation := BuildTopologyFromEntry(entry)
		if annotation == nil {
			continue
		}

		mergeTopology(exec.Topology, annotation)
	}

	applyDeclarativeConnections(exec.Topology, internaltopology.DefaultConnectionRegistry)
	NormalizeTopology(exec.Topology)
	FoldTopologyRisk(exec.Topology)
}

func applyDeclarativeConnections(
	t *types.RuntimeTopology,
	registry internaltopology.ConnectionRegistry,
) {
	if t == nil {
		return
	}

	seenNodes := make(map[types.RuntimeNodeID]struct{}, len(t.Nodes))
	queue := make([]types.RuntimeNode, 0, len(t.Nodes))
	for _, node := range t.Nodes {
		if _, seen := seenNodes[node.ID]; seen {
			continue
		}
		seenNodes[node.ID] = struct{}{}
		queue = append(queue, node)
	}

	seenEdges := make(map[types.RuntimeEdge]struct{}, len(t.Edges))
	for _, edge := range t.Edges {
		seenEdges[edge] = struct{}{}
	}

	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]

		definitions := registry.LookupByNodeID(node.ID)
		for _, capability := range node.Capabilities {
			definitions = append(definitions, registry.LookupByCapability(capability)...)
		}

		for _, definition := range definitions {
			edge := definition.EdgeFrom(node.ID)
			if _, seen := seenEdges[edge]; seen {
				continue
			}

			if _, exists := seenNodes[definition.TargetNodeID]; !exists {
				entry, ok := FindEntry(definition.TargetNodeID)
				if !ok {
					continue
				}

				targetNode := types.RuntimeNode{
					ID:   entry.NodeID,
					Role: entry.Role,
					Capabilities: append(
						[]types.RuntimeCapability(nil),
						entry.Capabilities...,
					),
					RiskLevel: entry.RiskLevel,
				}
				t.Nodes = append(t.Nodes, targetNode)
				seenNodes[targetNode.ID] = struct{}{}
				queue = append(queue, targetNode)
			}

			t.Edges = append(t.Edges, edge)
			seenEdges[edge] = struct{}{}
		}
	}
}

func detectedRuntimeEvidence(packages []types.Package) []types.RuntimeNodeID {
	names := make(map[string]struct{}, len(packages))
	for _, pkg := range packages {
		normalized := strings.ToLower(strings.TrimSpace(pkg.Id.Name.String()))
		if normalized == "" {
			continue
		}
		names[normalized] = struct{}{}
	}

	detected := make([]types.RuntimeNodeID, 0, 6)
	if hasAnyName(names, "sinytra-connector") {
		detected = append(detected, RuntimeNodeConnector)
	}
	if hasAnyName(names, "kilt") {
		detected = append(detected, RuntimeNodeKilt)
	}
	if hasAnyName(names, "velocity") {
		detected = append(detected, RuntimeNodeVelocity)
	}
	if hasAnyName(names, "bungeecord") {
		detected = append(detected, RuntimeNodeBungeecord)
	}
	if hasAnyName(names, "waterfall") {
		detected = append(detected, RuntimeNodeWaterfall)
	}
	if hasAnyName(names, "geyser", "geyser-spigot", "geyser-fabric") {
		detected = append(detected, RuntimeNodeGeyser)
	}
	if hasAnyName(names, "arclight") {
		detected = append(detected, RuntimeNodeArclight)
	}

	return detected
}

func inferHostTopologyFromAttachedBridgePackages(
	packages []types.Package,
) *types.RuntimeTopology {
	for _, pkg := range packages {
		name := strings.ToLower(strings.TrimSpace(pkg.Id.Name.String()))
		if name != "kilt" {
			continue
		}

		if pkg.Id.Platform != types.PlatformFabric {
			continue
		}

		entry, ok := FindEntry(RuntimeNodeFabric)
		if !ok {
			return nil
		}

		return BuildTopologyFromEntry(entry)
	}

	return nil
}

func detectedRuntimeEvidenceFromHints(hints []string) []types.RuntimeNodeID {
	if len(hints) == 0 {
		return nil
	}

	detected := make([]types.RuntimeNodeID, 0, len(hints))
	for _, hint := range hints {
		switch types.RuntimeNodeID(hint) {
		case RuntimeNodeConnector,
			RuntimeNodeKilt,
			RuntimeNodeVelocity,
			RuntimeNodeBungeecord,
			RuntimeNodeGeyser,
			RuntimeNodeGeyserStandalone:
			detected = append(detected, types.RuntimeNodeID(hint))
		}
	}

	return detected
}

func hasAnyName(names map[string]struct{}, candidates ...string) bool {
	for _, candidate := range candidates {
		if _, ok := names[candidate]; ok {
			return true
		}
	}

	return false
}

// NormalizeTopology deduplicates nodes (by ID, last-write wins) and edges
// (by From+To+Kind triple), then sorts both slices for deterministic output.
// Safe to call on nil or unresolved topologies.
func NormalizeTopology(t *types.RuntimeTopology) {
	if t == nil {
		return
	}

	seenNodes := make(map[types.RuntimeNodeID]int, len(t.Nodes))
	deduped := make([]types.RuntimeNode, 0, len(t.Nodes))
	for _, node := range t.Nodes {
		if idx, exists := seenNodes[node.ID]; exists {
			deduped[idx] = node
		} else {
			seenNodes[node.ID] = len(deduped)
			deduped = append(deduped, node)
		}
	}
	t.Nodes = deduped

	type edgeKey struct {
		From types.RuntimeNodeID
		To   types.RuntimeNodeID
		Kind types.RuntimeEdgeVerb
	}
	seenEdges := make(map[edgeKey]struct{}, len(t.Edges))
	dedupedEdges := make([]types.RuntimeEdge, 0, len(t.Edges))
	for _, edge := range t.Edges {
		key := edgeKey{From: edge.From, To: edge.To, Kind: edge.Verb}
		if _, exists := seenEdges[key]; exists {
			continue
		}
		seenEdges[key] = struct{}{}
		dedupedEdges = append(dedupedEdges, edge)
	}
	t.Edges = dedupedEdges

	sortTopology(t)
}

// FoldTopologyRisk propagates the maximum node risk level across all connected
// components by repeatedly folding each edge's endpoints to their maximum risk.
// Safe to call on nil or unresolved topologies.
func FoldTopologyRisk(t *types.RuntimeTopology) {
	if t == nil {
		return
	}

	nodeIndex := make(map[types.RuntimeNodeID]int, len(t.Nodes))
	for i, node := range t.Nodes {
		nodeIndex[node.ID] = i
	}

	changed := true
	for changed {
		changed = false
		for _, edge := range t.Edges {
			from, okFrom := nodeIndex[edge.From]
			to, okTo := nodeIndex[edge.To]
			if !okFrom || !okTo {
				continue
			}

			maxRisk := max(t.Nodes[from].RiskLevel, t.Nodes[to].RiskLevel)
			if t.Nodes[from].RiskLevel != maxRisk {
				t.Nodes[from].RiskLevel = maxRisk
				changed = true
			}
			if t.Nodes[to].RiskLevel != maxRisk {
				t.Nodes[to].RiskLevel = maxRisk
				changed = true
			}
		}
	}
}

func mergeTopology(dst *types.RuntimeTopology, src *types.RuntimeTopology) {
	if dst == nil || src == nil {
		return
	}

	seenNodes := make(
		map[types.RuntimeNodeID]struct{},
		len(dst.Nodes)+len(src.Nodes),
	)
	for _, node := range dst.Nodes {
		seenNodes[node.ID] = struct{}{}
	}

	for _, node := range src.Nodes {
		if _, exists := seenNodes[node.ID]; exists {
			continue
		}
		dst.Nodes = append(dst.Nodes, node)
		seenNodes[node.ID] = struct{}{}
	}

	seenEdges := make(
		map[types.RuntimeEdge]struct{},
		len(dst.Edges)+len(src.Edges),
	)
	for _, edge := range dst.Edges {
		seenEdges[edge] = struct{}{}
	}

	for _, edge := range src.Edges {
		if _, exists := seenEdges[edge]; exists {
			continue
		}
		dst.Edges = append(dst.Edges, edge)
		seenEdges[edge] = struct{}{}
	}

	sortTopology(dst)
}

func sortTopology(t *types.RuntimeTopology) {
	if t == nil {
		return
	}

	sort.Slice(
		t.Nodes, func(i, j int) bool {
			return string(t.Nodes[i].ID) < string(t.Nodes[j].ID)
		},
	)

	sort.Slice(
		t.Edges, func(i, j int) bool {
			if t.Edges[i].From != t.Edges[j].From {
				return string(t.Edges[i].From) < string(t.Edges[j].From)
			}
			if t.Edges[i].To != t.Edges[j].To {
				return string(t.Edges[i].To) < string(t.Edges[j].To)
			}
			return string(t.Edges[i].Verb) < string(t.Edges[j].Verb)
		},
	)
}
