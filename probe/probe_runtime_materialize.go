package probe

import (
	"strings"

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
		nodeID, ok := RuntimeIdentityNode(identity)
		if !ok {
			continue
		}

		entry, ok := FindEntry(nodeID)
		if !ok {
			continue
		}
		return BuildTopologyFromEntry(entry)
	}

	return nil
}

func RuntimeIdentityNode(identity types.PackageId) (types.RuntimeNodeID, bool) {
	switch strings.ToLower(strings.TrimSpace(string(identity.Name))) {
	case "fabric", "fabric-loader":
		return RuntimeNodeFabric, true
	case "forge":
		return RuntimeNodeForge, true
	case "neoforge":
		return RuntimeNodeNeoforge, true
	case "mcdreforged", "mcdr":
		return RuntimeNodeMCDR, true
	case "minecraft", "mc":
		return RuntimeNodeMinecraft, true
	default:
		return "", false
	}
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
