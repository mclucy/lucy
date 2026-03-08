package probe

import "github.com/mclucy/lucy/types"

const (
	RuntimeNodeMinecraft  types.RuntimeNodeID = "minecraft"
	RuntimeNodeFabric     types.RuntimeNodeID = "fabric"
	RuntimeNodeForge      types.RuntimeNodeID = "forge"
	RuntimeNodeNeoforge   types.RuntimeNodeID = "neoforge"
	RuntimeNodeMCDR       types.RuntimeNodeID = "mcdr"
	RuntimeNodePaper      types.RuntimeNodeID = "paper"
	RuntimeNodeSpigot     types.RuntimeNodeID = "spigot"
	RuntimeNodeBukkit     types.RuntimeNodeID = "bukkit"
	RuntimeNodeArclight   types.RuntimeNodeID = "arclight"
	RuntimeNodeYouer      types.RuntimeNodeID = "youer"
	RuntimeNodeVelocity   types.RuntimeNodeID = "velocity"
	RuntimeNodeBungeecord types.RuntimeNodeID = "bungeecord"
	RuntimeNodeWaterfall  types.RuntimeNodeID = "waterfall"
	RuntimeNodeGeyser     types.RuntimeNodeID = "geyser"
	RuntimeNodeConnector  types.RuntimeNodeID = "connector"
	RuntimeNodeKilt       types.RuntimeNodeID = "kilt"
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
	},
	{
		NodeID:           RuntimeNodeSpigot,
		Role:             types.RuntimeRolePluginCore,
		IdentityPlatform: types.PlatformAny,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityBukkitPlugins,
		},
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
		},
	},
	{
		NodeID:           RuntimeNodeBungeecord,
		Role:             types.RuntimeRoleProxy,
		IdentityPlatform: types.PlatformAny,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityProxying,
		},
	},
	{
		NodeID:           RuntimeNodeWaterfall,
		Role:             types.RuntimeRoleProxy,
		IdentityPlatform: types.PlatformAny,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityProxying,
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
		PolicyEdges: []RegistryEdge{
			{
				TargetNodeID: RuntimeNodeForge,
				Kind:         types.EdgeBridges,
				Risk:         types.RiskHigh,
			},
		},
	},
	{
		NodeID:           RuntimeNodeKilt,
		Role:             types.RuntimeRoleBridge,
		IdentityPlatform: types.PlatformAny,
		RiskLevel:        types.RiskHigh,
		PolicyEdges: []RegistryEdge{
			{
				TargetNodeID: RuntimeNodeForge,
				Kind:         types.EdgeBridges,
				Risk:         types.RiskHigh,
			},
		},
	},
}
