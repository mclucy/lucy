package probe

import (
	"github.com/mclucy/lucy/probe/internal/detector"
	"github.com/mclucy/lucy/types"
)

func materializeRuntimeInfo(evidence *detector.ExecutableEvidence) *types.RuntimeInfo {
	if evidence == nil {
		return nil
	}

	return &types.RuntimeInfo{
		PrimaryEntrance:   evidence.PrimaryEntrance,
		GameVersion:       evidence.GameVersion,
		BootCommand:       nil,
		Topology:          materializeRuntimeTopology(evidence),
		RuntimeIdentities: append([]types.PackageId(nil), evidence.RuntimeIdentities...),
		BridgeHints:       append([]string(nil), evidence.BridgeHints...),
	}
}

func materializeRuntimeTopology(
	evidence *detector.ExecutableEvidence,
) *types.RuntimeTopology {
	if evidence == nil {
		return nil
	}

	if evidence.Topology != nil {
		return cloneRuntimeTopology(evidence.Topology)
	}

	if evidence.TopologySeed != nil {
		return &types.RuntimeTopology{
			PrimaryNode: evidence.TopologySeed.PrimaryNode,
			Nodes:       append([]types.RuntimeNode(nil), evidence.TopologySeed.Nodes...),
			Edges:       append([]types.RuntimeEdge(nil), evidence.TopologySeed.Edges...),
		}
	}

	for _, identity := range evidence.RuntimeIdentities {
		entry, ok := LookupByPlatform(identity.Platform)
		if !ok {
			continue
		}
		return BuildTopologyFromEntry(entry)
	}

	return nil
}

func cloneRuntimeTopology(topology *types.RuntimeTopology) *types.RuntimeTopology {
	if topology == nil {
		return nil
	}

	return &types.RuntimeTopology{
		PrimaryNode: topology.PrimaryNode,
		Nodes:       append([]types.RuntimeNode(nil), topology.Nodes...),
		Edges:       append([]types.RuntimeEdge(nil), topology.Edges...),
	}
}
