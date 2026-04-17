package probe

import "github.com/mclucy/lucy/types"

// probe_topology_data.go is the pure declarative source of truth for modellable
// runtime topology families, nodes, relationships, and capabilities. It must
// not contain probe/evidence parsing logic, detection heuristics, or status
// rendering strings.

const (
	RuntimeNodeMinecraft        types.RuntimeNodeID = "minecraft"
	RuntimeNodeFabric           types.RuntimeNodeID = "fabric"
	RuntimeNodeForge            types.RuntimeNodeID = "forge"
	RuntimeNodeNeoforge         types.RuntimeNodeID = "neoforge"
	RuntimeNodeMCDR             types.RuntimeNodeID = "mcdr"
	RuntimeNodePaper            types.RuntimeNodeID = "paper"
	RuntimeNodeSpigot           types.RuntimeNodeID = "spigot"
	RuntimeNodePaperFork        types.RuntimeNodeID = "paper-fork"
	RuntimeNodeCraftBukkit      types.RuntimeNodeID = "craftbukkit"
	RuntimeNodeBukkit           types.RuntimeNodeID = "bukkit"
	RuntimeNodeFolia            types.RuntimeNodeID = "folia"
	RuntimeNodeLeaves           types.RuntimeNodeID = "leaves"
	RuntimeNodeSponge           types.RuntimeNodeID = "sponge"
	RuntimeNodeArclight         types.RuntimeNodeID = "arclight"
	RuntimeNodeYouer            types.RuntimeNodeID = "youer"
	RuntimeNodeVelocity         types.RuntimeNodeID = "velocity"
	RuntimeNodeBungeecord       types.RuntimeNodeID = "bungeecord"
	RuntimeNodeWaterfall        types.RuntimeNodeID = "waterfall"
	RuntimeNodeGeyser           types.RuntimeNodeID = "geyser"
	RuntimeNodeGeyserStandalone types.RuntimeNodeID = "geyser_standalone"
	RuntimeNodeConnector        types.RuntimeNodeID = "connector"
	RuntimeNodeKilt             types.RuntimeNodeID = "kilt"
)

var defaultRegistryEntries = []RegistryEntry{
	{
		NodeID:           RuntimeNodeMinecraft,
		Role:             types.RuntimeRoleVanilla,
		IdentityPlatform: types.PlatformMinecraft,
	},
	{
		NodeID:           RuntimeNodeFabric,
		Role:             types.RuntimeRoleModLoader,
		IdentityPlatform: types.PlatformFabric,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityFabricMods,
		},
	},
	{
		NodeID:           RuntimeNodeForge,
		Role:             types.RuntimeRoleModLoader,
		IdentityPlatform: types.PlatformForge,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityForgeMods,
		},
	},
	{
		NodeID:           RuntimeNodeNeoforge,
		Role:             types.RuntimeRoleModLoader,
		IdentityPlatform: types.PlatformNeoforge,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityNeoforgeMods,
		},
	},
	{
		NodeID:           RuntimeNodeMCDR,
		Role:             types.RuntimeRolePluginCore,
		IdentityPlatform: types.PlatformMCDR,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityMCDRPlugins,
		},
	},
	{
		NodeID:           RuntimeNodePaper,
		Role:             types.RuntimeRolePluginCore,
		IdentityPlatform: types.PlatformAny,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityBukkitPlugins,
		},
		// Paper stays anchored to vanilla while Bukkit-family ancestry is folded into
		// the node's own semantics.
		PolicyEdges: []RegistryEdge{
			{
				TargetNodeID: RuntimeNodeMinecraft,
				Kind:         types.EdgeModifies,
				Risk:         types.RiskNone,
			},
		},
	},
	{
		NodeID:           RuntimeNodePaperFork,
		Role:             types.RuntimeRolePluginCore,
		IdentityPlatform: types.PlatformAny,
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
				Risk:         types.RiskNone,
			},
		},
	},
	{
		NodeID:           RuntimeNodeSpigot,
		Role:             types.RuntimeRolePluginCore,
		IdentityPlatform: types.PlatformAny,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityBukkitPlugins,
		},
		// Spigot stays anchored directly to vanilla rather than expanding the old
		// CraftBukkit lineage chain into separate runtime facts.
		PolicyEdges: []RegistryEdge{
			{
				TargetNodeID: RuntimeNodeMinecraft,
				Kind:         types.EdgeModifies,
				Risk:         types.RiskNone,
			},
		},
	},
	{
		NodeID:           RuntimeNodeCraftBukkit,
		Role:             types.RuntimeRolePluginCore,
		IdentityPlatform: types.PlatformAny,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityBukkitPlugins,
		},
		// CraftBukkit is still a concrete implementation identity, so it anchors back
		// to vanilla without reviving intermediate lineage edges.
		PolicyEdges: []RegistryEdge{{
			TargetNodeID: RuntimeNodeMinecraft,
			Kind:         types.EdgeModifies,
			Risk:         types.RiskNone,
		}},
	},
	{
		NodeID:           RuntimeNodeBukkit,
		Role:             types.RuntimeRolePluginCore,
		IdentityPlatform: types.PlatformAny,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityBukkitPlugins,
		},
	},
	{
		NodeID:           RuntimeNodeFolia,
		Role:             types.RuntimeRolePluginCore,
		IdentityPlatform: types.PlatformAny,
		RiskLevel:        types.RiskMedium,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityBukkitPlugins,
		},
		PolicyEdges: []RegistryEdge{{
			TargetNodeID: RuntimeNodePaper,
			Kind:         types.EdgeImplements,
			Risk:         types.RiskMedium,
		}},
	},
	{
		NodeID:           RuntimeNodeLeaves,
		Role:             types.RuntimeRolePluginCore,
		IdentityPlatform: types.PlatformAny,
		RiskLevel:        types.RiskNone,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityBukkitPlugins,
		},
		PolicyEdges: []RegistryEdge{{
			TargetNodeID: RuntimeNodePaper,
			Kind:         types.EdgeImplements,
			Risk:         types.RiskNone,
		}},
	},
	{
		NodeID:           RuntimeNodeSponge,
		Role:             types.RuntimeRolePluginCore,
		IdentityPlatform: types.PlatformAny,
		Capabilities: []types.RuntimeCapability{
			types.CapabilitySpongePlugins,
		},
	},
	{
		NodeID:           RuntimeNodeArclight,
		Role:             types.RuntimeRoleHybrid,
		IdentityPlatform: types.PlatformAny,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityForgeMods,
			types.CapabilityBukkitPlugins,
		},
	},
	{
		NodeID:           RuntimeNodeYouer,
		Role:             types.RuntimeRoleHybrid,
		IdentityPlatform: types.PlatformAny,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityNeoforgeMods,
			types.CapabilityBukkitPlugins,
		},
	},
	{
		NodeID:           RuntimeNodeVelocity,
		Role:             types.RuntimeRoleProxy,
		IdentityPlatform: types.PlatformAny,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityProxying,
			types.CapabilityVelocityPlugins,
		},
	},
	{
		NodeID:           RuntimeNodeBungeecord,
		Role:             types.RuntimeRoleProxy,
		IdentityPlatform: types.PlatformAny,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityProxying,
			types.CapabilityBungeecordPlugins,
		},
	},
	{
		NodeID:           RuntimeNodeWaterfall,
		Role:             types.RuntimeRoleProxy,
		IdentityPlatform: types.PlatformAny,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityProxying,
			types.CapabilityBungeecordPlugins,
		},
	},
	{
		NodeID:           RuntimeNodeGeyserStandalone,
		Role:             types.RuntimeRoleProxy,
		IdentityPlatform: types.PlatformAny,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityProxying,
			types.CapabilityProtocolBridge,
		},
	},
	{
		NodeID:           RuntimeNodeGeyser,
		Role:             types.RuntimeRoleProtocolBridge,
		IdentityPlatform: types.PlatformAny,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityProtocolBridge,
		},
	},
	{
		NodeID:           RuntimeNodeConnector,
		Role:             types.RuntimeRoleBridge,
		IdentityPlatform: types.PlatformAny,
		RiskLevel:        types.RiskHigh,
	},
	{
		NodeID:           RuntimeNodeKilt,
		Role:             types.RuntimeRoleBridge,
		IdentityPlatform: types.PlatformAny,
		RiskLevel:        types.RiskHigh,
	},
}
