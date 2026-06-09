package probe

import "github.com/mclucy/lucy/types"

// probe_topology_data.go is the pure declarative source of truth for modellable
// runtime topology families, nodes, relationships, and capabilities. It must
// not contain probe/evidence parsing logic, detection heuristics, or status
// rendering strings.

var defaultRegistryEntries = []RegistryEntry{
	{
		NodeID: types.RuntimeNodeMinecraft,
		Role:   types.RuntimeRoleVanilla,
	},
	{
		NodeID: types.RuntimeNodeFabric,
		Role:   types.RuntimeRoleModLoader,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityFabricMods,
		},
	},
	{
		NodeID: types.RuntimeNodeForge,
		Role:   types.RuntimeRoleModLoader,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityForgeMods,
		},
	},
	{
		NodeID: types.RuntimeNodeNeoforge,
		Role:   types.RuntimeRoleModLoader,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityNeoforgeMods,
		},
	},
	{
		NodeID: types.RuntimeNodeMCDR,
		Role:   types.RuntimeRolePluginCore,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityMCDRPlugins,
		},
	},
	{
		NodeID: types.RuntimeNodePaper,
		Role:   types.RuntimeRolePluginCore,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityBukkitPlugins,
		},
		// Paper stays anchored to vanilla while Bukkit-family ancestry is folded into
		// the node's own semantics.
		PolicyEdges: []RegistryEdge{
			{
				TargetNodeID: types.RuntimeNodeMinecraft,
				Kind:         types.EdgeModifies,
			},
		},
	},
	{
		NodeID: types.RuntimeNodePaperFork,
		Role:   types.RuntimeRolePluginCore,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityBukkitPlugins,
		},
		// paper-fork is the extensible, best-effort tier for public Paper forks.
		// Its primary guarantee is that it hosts the same plugin ecosystem as Paper.
		// Surface only the detectable fork relationship back to Paper.
		PolicyEdges: []RegistryEdge{
			{
				TargetNodeID: types.RuntimeNodePaper,
				Kind:         types.EdgeImplements,
			},
		},
	},
	{
		NodeID: types.RuntimeNodeSpigot,
		Role:   types.RuntimeRolePluginCore,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityBukkitPlugins,
		},
		// Spigot stays anchored directly to vanilla rather than expanding the old
		// CraftBukkit lineage chain into separate runtime facts.
		PolicyEdges: []RegistryEdge{
			{
				TargetNodeID: types.RuntimeNodeMinecraft,
				Kind:         types.EdgeModifies,
			},
		},
	},
	{
		NodeID: types.RuntimeNodeCraftBukkit,
		Role:   types.RuntimeRolePluginCore,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityBukkitPlugins,
		},
		// CraftBukkit is still a concrete implementation identity, so it anchors back
		// to vanilla without reviving intermediate lineage edges.
		PolicyEdges: []RegistryEdge{{
			TargetNodeID: types.RuntimeNodeMinecraft,
			Kind:         types.EdgeModifies,
		}},
	},
	{
		NodeID: types.RuntimeNodeBukkit,
		Role:   types.RuntimeRolePluginCore,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityBukkitPlugins,
		},
	},
	{
		NodeID:    types.RuntimeNodeFolia,
		Role:      types.RuntimeRolePluginCore,
		RiskLevel: types.RiskMedium,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityBukkitPlugins,
		},
		PolicyEdges: []RegistryEdge{{
			TargetNodeID: types.RuntimeNodePaper,
			Kind:         types.EdgeImplements,
		}},
	},
	{
		NodeID:    types.RuntimeNodeLeaves,
		Role:      types.RuntimeRolePluginCore,
		RiskLevel: types.RiskNone,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityBukkitPlugins,
		},
		PolicyEdges: []RegistryEdge{{
			TargetNodeID: types.RuntimeNodePaper,
			Kind:         types.EdgeImplements,
		}},
	},
	{
		NodeID: types.RuntimeNodeSponge,
		Role:   types.RuntimeRolePluginCore,
		Capabilities: []types.RuntimeCapability{
			types.CapabilitySpongePlugins,
		},
	},
	{
		NodeID: types.RuntimeNodeArclight,
		Role:   types.RuntimeRoleHybrid,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityForgeMods,
			types.CapabilityBukkitPlugins,
		},
	},
	{
		NodeID: types.RuntimeNodeYouer,
		Role:   types.RuntimeRoleHybrid,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityNeoforgeMods,
			types.CapabilityBukkitPlugins,
		},
	},
	{
		NodeID: types.RuntimeNodeVelocity,
		Role:   types.RuntimeRoleProxy,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityProxying,
			types.CapabilityVelocityPlugins,
		},
	},
	{
		NodeID: types.RuntimeNodeBungeecord,
		Role:   types.RuntimeRoleProxy,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityProxying,
			types.CapabilityBungeecordPlugins,
		},
	},
	{
		NodeID: types.RuntimeNodeWaterfall,
		Role:   types.RuntimeRoleProxy,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityProxying,
			types.CapabilityBungeecordPlugins,
		},
	},
	{
		NodeID: types.RuntimeNodeGeyserStandalone,
		Role:   types.RuntimeRoleProxy,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityProxying,
			types.CapabilityProtocolBridge,
		},
	},
	{
		NodeID: types.RuntimeNodeGeyser,
		Role:   types.RuntimeRoleProtocolBridge,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityProtocolBridge,
		},
	},
	{
		NodeID:    types.RuntimeNodeConnector,
		Role:      types.RuntimeRoleBridge,
		RiskLevel: types.RiskHigh,
	},
	{
		NodeID:    types.RuntimeNodeKilt,
		Role:      types.RuntimeRoleBridge,
		RiskLevel: types.RiskHigh,
	},
}
