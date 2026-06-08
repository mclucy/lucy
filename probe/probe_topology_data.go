package probe

import "github.com/mclucy/lucy/types"

// probe_topology_data.go is the pure declarative source of truth for modellable
// runtime topology families, nodes, relationships, and capabilities. It must
// not contain probe/evidence parsing logic, detection heuristics, or status
// rendering strings.

const (
	RuntimeNodeMinecraft        = types.RuntimeNodeMinecraft
	RuntimeNodeFabric           = types.RuntimeNodeFabric
	RuntimeNodeForge            = types.RuntimeNodeForge
	RuntimeNodeNeoforge         = types.RuntimeNodeNeoforge
	RuntimeNodeMCDR             = types.RuntimeNodeMCDR
	RuntimeNodePaper            = types.RuntimeNodePaper
	RuntimeNodeSpigot           = types.RuntimeNodeSpigot
	RuntimeNodePaperFork        = types.RuntimeNodePaperFork
	RuntimeNodeCraftBukkit      = types.RuntimeNodeCraftBukkit
	RuntimeNodeBukkit           = types.RuntimeNodeBukkit
	RuntimeNodeFolia            = types.RuntimeNodeFolia
	RuntimeNodeLeaves           = types.RuntimeNodeLeaves
	RuntimeNodeSponge           = types.RuntimeNodeSponge
	RuntimeNodeArclight         = types.RuntimeNodeArclight
	RuntimeNodeYouer            = types.RuntimeNodeYouer
	RuntimeNodeVelocity         = types.RuntimeNodeVelocity
	RuntimeNodeBungeecord       = types.RuntimeNodeBungeecord
	RuntimeNodeWaterfall        = types.RuntimeNodeWaterfall
	RuntimeNodeGeyser           = types.RuntimeNodeGeyser
	RuntimeNodeGeyserStandalone = types.RuntimeNodeGeyserStandalone
	RuntimeNodeConnector        = types.RuntimeNodeConnector
	RuntimeNodeKilt             = types.RuntimeNodeKilt
)

var defaultRegistryEntries = []RegistryEntry{
	{
		NodeID: RuntimeNodeMinecraft,
		Role:   types.RuntimeRoleVanilla,
	},
	{
		NodeID: RuntimeNodeFabric,
		Role:   types.RuntimeRoleModLoader,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityFabricMods,
		},
	},
	{
		NodeID: RuntimeNodeForge,
		Role:   types.RuntimeRoleModLoader,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityForgeMods,
		},
	},
	{
		NodeID: RuntimeNodeNeoforge,
		Role:   types.RuntimeRoleModLoader,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityNeoforgeMods,
		},
	},
	{
		NodeID: RuntimeNodeMCDR,
		Role:   types.RuntimeRolePluginCore,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityMCDRPlugins,
		},
	},
	{
		NodeID: RuntimeNodePaper,
		Role:   types.RuntimeRolePluginCore,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityBukkitPlugins,
		},
		// Paper stays anchored to vanilla while Bukkit-family ancestry is folded into
		// the node's own semantics.
		PolicyEdges: []RegistryEdge{
			{
				TargetNodeID: RuntimeNodeMinecraft,
				Kind:         types.EdgeModifies,
			},
		},
	},
	{
		NodeID: RuntimeNodePaperFork,
		Role:   types.RuntimeRolePluginCore,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityBukkitPlugins,
		},
		// paper-fork is the extensible, best-effort tier for public Paper forks.
		// Its primary guarantee is that it hosts the same plugin ecosystem as Paper.
		// Surface only the detectable fork relationship back to Paper.
		PolicyEdges: []RegistryEdge{
			{
				TargetNodeID: RuntimeNodePaper,
				Kind:         types.EdgeImplements,
			},
		},
	},
	{
		NodeID: RuntimeNodeSpigot,
		Role:   types.RuntimeRolePluginCore,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityBukkitPlugins,
		},
		// Spigot stays anchored directly to vanilla rather than expanding the old
		// CraftBukkit lineage chain into separate runtime facts.
		PolicyEdges: []RegistryEdge{
			{
				TargetNodeID: RuntimeNodeMinecraft,
				Kind:         types.EdgeModifies,
			},
		},
	},
	{
		NodeID: RuntimeNodeCraftBukkit,
		Role:   types.RuntimeRolePluginCore,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityBukkitPlugins,
		},
		// CraftBukkit is still a concrete implementation identity, so it anchors back
		// to vanilla without reviving intermediate lineage edges.
		PolicyEdges: []RegistryEdge{{
			TargetNodeID: RuntimeNodeMinecraft,
			Kind:         types.EdgeModifies,
		}},
	},
	{
		NodeID: RuntimeNodeBukkit,
		Role:   types.RuntimeRolePluginCore,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityBukkitPlugins,
		},
	},
	{
		NodeID:    RuntimeNodeFolia,
		Role:      types.RuntimeRolePluginCore,
		RiskLevel: types.RiskMedium,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityBukkitPlugins,
		},
		PolicyEdges: []RegistryEdge{{
			TargetNodeID: RuntimeNodePaper,
			Kind:         types.EdgeImplements,
		}},
	},
	{
		NodeID:    RuntimeNodeLeaves,
		Role:      types.RuntimeRolePluginCore,
		RiskLevel: types.RiskNone,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityBukkitPlugins,
		},
		PolicyEdges: []RegistryEdge{{
			TargetNodeID: RuntimeNodePaper,
			Kind:         types.EdgeImplements,
		}},
	},
	{
		NodeID: RuntimeNodeSponge,
		Role:   types.RuntimeRolePluginCore,
		Capabilities: []types.RuntimeCapability{
			types.CapabilitySpongePlugins,
		},
	},
	{
		NodeID: RuntimeNodeArclight,
		Role:   types.RuntimeRoleHybrid,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityForgeMods,
			types.CapabilityBukkitPlugins,
		},
	},
	{
		NodeID: RuntimeNodeYouer,
		Role:   types.RuntimeRoleHybrid,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityNeoforgeMods,
			types.CapabilityBukkitPlugins,
		},
	},
	{
		NodeID: RuntimeNodeVelocity,
		Role:   types.RuntimeRoleProxy,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityProxying,
			types.CapabilityVelocityPlugins,
		},
	},
	{
		NodeID: RuntimeNodeBungeecord,
		Role:   types.RuntimeRoleProxy,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityProxying,
			types.CapabilityBungeecordPlugins,
		},
	},
	{
		NodeID: RuntimeNodeWaterfall,
		Role:   types.RuntimeRoleProxy,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityProxying,
			types.CapabilityBungeecordPlugins,
		},
	},
	{
		NodeID: RuntimeNodeGeyserStandalone,
		Role:   types.RuntimeRoleProxy,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityProxying,
			types.CapabilityProtocolBridge,
		},
	},
	{
		NodeID: RuntimeNodeGeyser,
		Role:   types.RuntimeRoleProtocolBridge,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityProtocolBridge,
		},
	},
	{
		NodeID:    RuntimeNodeConnector,
		Role:      types.RuntimeRoleBridge,
		RiskLevel: types.RiskHigh,
	},
	{
		NodeID:    RuntimeNodeKilt,
		Role:      types.RuntimeRoleBridge,
		RiskLevel: types.RiskHigh,
	},
}
