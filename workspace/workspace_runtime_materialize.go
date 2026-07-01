package workspace

import (
	"fmt"
	"strings"

	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/workspace/internal/detector"
)

func materializeRuntimeInfo(evidence *detector.ExecutableEvidence) *ServerInstance {
	if evidence == nil {
		return nil
	}

	return &ServerInstance{
		PrimaryEntrance: evidence.PrimaryEntrance,
		GameVersion:     evidence.GameVersion,
		BootCommand:     nil,
		topology:        materializeRuntimeTopology(evidence),
	}
}

func materializeRuntimeTopology(
	evidence *detector.ExecutableEvidence,
) *types.ServerTopology {
	if evidence == nil {
		return nil
	}

	var topology *types.ServerTopology

	switch {
	case evidence.Topology != nil:
		topology = cloneRuntimeTopology(evidence.Topology)
	case evidence.TopologySeed != nil:
		topology = &types.ServerTopology{
			PrimaryNode: evidence.TopologySeed.PrimaryNode,
			Nodes: append(
				[]types.RuntimeNode(nil),
				evidence.TopologySeed.Nodes...,
			),
			Edges: append(
				[]types.RuntimeEdge(nil),
				evidence.TopologySeed.Edges...,
			),
		}
	default:
		for _, identity := range evidence.RuntimeIdentities {
			nodeID, ok := RuntimeIdentityNode(identity)
			if !ok {
				continue
			}

			entry, ok := FindEntry(nodeID)
			if !ok {
				continue
			}
			topology = BuildTopologyFromEntry(entry)
			break
		}
	}

	if topology == nil {
		return nil
	}

	distributeRuntimeIdentities(topology, evidence.RuntimeIdentities)
	return topology
}

func distributeRuntimeIdentities(
	topology *types.ServerTopology,
	identities []types.VersionedPackageRef,
) {
	if topology == nil {
		return
	}

	for _, identity := range identities {
		nodeID, ok := RuntimeIdentityNode(identity)
		if !ok {
			log.Warn(
				fmt.Errorf(
					"unmatched runtime identity %q: no node mapping",
					identity.Name,
				),
			)
			continue
		}

		index := -1
		for i := range topology.Nodes {
			if topology.Nodes[i].ID == nodeID {
				index = i
				break
			}
		}
		if index < 0 {
			log.Warn(
				fmt.Errorf(
					"unmatched runtime identity %q: topology has no node %q",
					identity.Name,
					nodeID,
				),
			)
			continue
		}

		topology.Nodes[index].Identities = append(
			topology.Nodes[index].Identities,
			identity,
		)
	}
}

func RuntimeIdentityNode(identity types.VersionedPackageRef) (
	types.RuntimeNodeID,
	bool,
) {
	switch strings.ToLower(strings.TrimSpace(string(identity.Name))) {
	case "fabric", "fabric-loader":
		return types.RuntimeNodeFabric, true
	case "forge":
		return types.RuntimeNodeForge, true
	case "neoforge":
		return types.RuntimeNodeNeoforge, true
	case "mcdreforged", "mcdr":
		return types.RuntimeNodeMCDR, true
	case "minecraft", "mc":
		return types.RuntimeNodeMinecraft, true
	case "connector", "sinytra-connector", "connectormod":
		return types.RuntimeNodeConnector, true
	case "paper":
		return types.RuntimeNodePaper, true
	case "paper-fork", "divine", "leaf":
		return types.RuntimeNodePaperFork, true
	case "bukkit":
		return types.RuntimeNodeBukkit, true
	case "craftbukkit":
		return types.RuntimeNodeCraftBukkit, true
	case "spigot":
		return types.RuntimeNodeSpigot, true
	case "folia":
		return types.RuntimeNodeFolia, true
	case "leaves":
		return types.RuntimeNodeLeaves, true
	case "youer":
		return types.RuntimeNodeYouer, true
	case "purpur", "reaper":
		return types.RuntimeNodePaperFork, true
	case "arclight":
		return types.RuntimeNodeArclight, true
	case "sponge":
		return types.RuntimeNodeSponge, true
	case "geyser-spigot", "geyser-fabric":
		return types.RuntimeNodeGeyser, true
	case "geyser":
		return types.RuntimeNodeGeyserStandalone, true
	default:
		return "", false
	}
}

func cloneRuntimeTopology(topology *types.ServerTopology) *types.ServerTopology {
	if topology == nil {
		return nil
	}

	// Keep cloned node identities separate before adding more.
	nodes := append([]types.RuntimeNode(nil), topology.Nodes...)
	for i := range nodes {
		nodes[i].Identities = append(
			[]types.VersionedPackageRef(nil),
			nodes[i].Identities...,
		)
	}

	return &types.ServerTopology{
		PrimaryNode: topology.PrimaryNode,
		Nodes:       nodes,
		Edges:       append([]types.RuntimeEdge(nil), topology.Edges...),
	}
}
