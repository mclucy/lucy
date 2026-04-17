package probe

import (
	internaltopology "github.com/mclucy/lucy/probe/internal/topology"
	"github.com/mclucy/lucy/types"
)

type RegistryEntry struct {
	NodeID       types.RuntimeNodeID
	Role         types.RuntimeRole
	Capabilities []types.RuntimeCapability
	RiskLevel    types.RuntimeRiskLevel
	PolicyEdges  []RegistryEdge
}

type RegistryEdge struct {
	TargetNodeID types.RuntimeNodeID
	Kind         types.RuntimeEdgeVerb
	Risk         types.RuntimeRiskLevel
}

type RuntimeRegistry struct {
	byID map[types.RuntimeNodeID]RegistryEntry
}

var DefaultRegistry = NewRuntimeRegistry(defaultRegistryEntries)

func NewRuntimeRegistry(entries []RegistryEntry) RuntimeRegistry {
	registry := RuntimeRegistry{
		byID: make(map[types.RuntimeNodeID]RegistryEntry, len(entries)),
	}

	for _, entry := range entries {
		stored := RegistryEntry{
			NodeID:       entry.NodeID,
			Role:         entry.Role,
			Capabilities: append([]types.RuntimeCapability(nil), entry.Capabilities...),
			RiskLevel:    entry.RiskLevel,
			PolicyEdges:  append([]RegistryEdge(nil), entry.PolicyEdges...),
		}

		registry.byID[stored.NodeID] = stored
	}

	return registry
}

func (r RuntimeRegistry) FindEntry(id types.RuntimeNodeID) (RegistryEntry, bool) {
	entry, ok := r.byID[id]
	if !ok {
		return RegistryEntry{}, false
	}

	return cloneEntry(entry), true
}

func FindEntry(id types.RuntimeNodeID) (RegistryEntry, bool) {
	return DefaultRegistry.FindEntry(id)
}

// BuildTopologyFromEntry constructs a RuntimeTopology with a single primary node
// from a registry entry, plus any policy edges listed.
func BuildTopologyFromEntry(entry RegistryEntry) *types.RuntimeTopology {
	if entry.NodeID == types.RuntimeNodeUnknown {
		return &types.RuntimeTopology{}
	}

	nodes := []types.RuntimeNode{{
		ID:           entry.NodeID,
		Role:         entry.Role,
		Capabilities: append([]types.RuntimeCapability(nil), entry.Capabilities...),
		RiskLevel:    entry.RiskLevel,
	}}

	edges := make([]types.RuntimeEdge, 0, len(entry.PolicyEdges))
	seenNode := map[types.RuntimeNodeID]struct{}{entry.NodeID: {}}

	for _, policyEdge := range entry.PolicyEdges {
		edges = append(edges, types.RuntimeEdge{
			From: entry.NodeID,
			To:   policyEdge.TargetNodeID,
			Verb: policyEdge.Kind,
			Risk: policyEdge.Risk,
		})

		if _, alreadyAdded := seenNode[policyEdge.TargetNodeID]; alreadyAdded {
			continue
		}

		target, ok := FindEntry(policyEdge.TargetNodeID)
		if !ok {
			continue
		}

		nodes = append(nodes, types.RuntimeNode{
			ID:           target.NodeID,
			Role:         target.Role,
			Capabilities: append([]types.RuntimeCapability(nil), target.Capabilities...),
			RiskLevel:    target.RiskLevel,
		})
		seenNode[policyEdge.TargetNodeID] = struct{}{}
	}

	topology := &types.RuntimeTopology{
		PrimaryNode: entry.NodeID,
		Nodes:       nodes,
		Edges:       edges,
	}

	applyDeclarativeConnections(topology, internaltopology.DefaultConnectionRegistry)
	NormalizeTopology(topology)

	return topology
}

func cloneEntry(entry RegistryEntry) RegistryEntry {
	return RegistryEntry{
		NodeID:       entry.NodeID,
		Role:         entry.Role,
		Capabilities: append([]types.RuntimeCapability(nil), entry.Capabilities...),
		RiskLevel:    entry.RiskLevel,
		PolicyEdges:  append([]RegistryEdge(nil), entry.PolicyEdges...),
	}
}
