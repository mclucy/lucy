package probe

import (
	"sort"
	"strings"

	"github.com/mclucy/lucy/types"
)

func EnrichTopologyFromPackages(
	exec *types.RuntimeInfo,
	packages []types.Package,
) {
	if exec == nil {
		return
	}

	evidence := detectedRuntimeEvidence(packages)

	if exec.Topology == nil {
		// No topology yet — attempt to build one from package evidence.
		if len(evidence) == 0 {
			exec.Topology = &types.RuntimeTopology{}
			return
		}

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

		for _, nodeID := range evidence[1:] {
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

		NormalizeTopology(exec.Topology)
		return
	}

	// Topology exists (resolved or not) — enrich with package evidence.
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

	// Also process bridge hints from JAR scanning.
	for _, hint := range exec.BridgeHints {
		nodeID := types.RuntimeNodeID(hint)
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

	NormalizeTopology(exec.Topology)
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
		Kind types.RuntimeEdgeKind
	}
	seenEdges := make(map[edgeKey]struct{}, len(t.Edges))
	dedupedEdges := make([]types.RuntimeEdge, 0, len(t.Edges))
	for _, edge := range t.Edges {
		key := edgeKey{From: edge.From, To: edge.To, Kind: edge.Kind}
		if _, exists := seenEdges[key]; exists {
			continue
		}
		seenEdges[key] = struct{}{}
		dedupedEdges = append(dedupedEdges, edge)
	}
	t.Edges = dedupedEdges

	sortTopology(t)
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
			return string(t.Edges[i].Kind) < string(t.Edges[j].Kind)
		},
	)
}
