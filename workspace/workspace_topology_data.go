package workspace

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
		NodeID:       types.RuntimeNodePaper,
		Role:         types.RuntimeRolePluginCore,
		Capabilities: types.CapabilityPaperPlugins.Populate(),
		// Paper stays anchored to vanilla while Bukkit-family ancestry is folded into
		// the node's own semantics via Populate().
		PolicyEdges: []RegistryEdge{
			{
				TargetNodeID: types.RuntimeNodeMinecraft,
				Kind:         types.EdgeExtends,
			},
		},
	},
	{
		NodeID:       types.RuntimeNodePaperFork,
		Role:         types.RuntimeRolePluginCore,
		Capabilities: types.CapabilityPurpurPlugins.Populate(),
		// paper-fork is the extensible, best-effort tier for public Paper forks
		// that extend the full Purpur API surface (Purpur itself, Folia without
		// the threading model, Leaves, Leaf, etc.). Declared rung is Purpur;
		// Populate() expands the lower Bukkit/Spigot/Paper ancestry.
		PolicyEdges: []RegistryEdge{
			{
				TargetNodeID: types.RuntimeNodePaper,
				Kind:         types.EdgeExtends,
			},
		},
	},
	{
		NodeID:       types.RuntimeNodeSpigot,
		Role:         types.RuntimeRolePluginCore,
		Capabilities: types.CapabilitySpigotPlugins.Populate(),
		// Spigot stays anchored directly to vanilla rather than expanding the old
		// CraftBukkit lineage chain into separate runtime facts.
		PolicyEdges: []RegistryEdge{
			{
				TargetNodeID: types.RuntimeNodeMinecraft,
				Kind:         types.EdgeExtends,
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
		PolicyEdges: []RegistryEdge{
			{
				TargetNodeID: types.RuntimeNodeMinecraft,
				Kind:         types.EdgeExtends,
			},
		},
	},
	{
		NodeID: types.RuntimeNodeBukkit,
		Role:   types.RuntimeRolePluginCore,
		Capabilities: []types.RuntimeCapability{
			types.CapabilityBukkitPlugins,
		},
	},
	{
		NodeID:       types.RuntimeNodeFolia,
		Role:         types.RuntimeRolePluginCore,
		RiskLevel:    types.RiskMedium,
		Capabilities: types.CapabilityFoliaPlugins.Populate(),
		// Folia inverts compatibility: Paper plugins may fail to load without the
		// `folia-supported: true` opt-in. Populate() still expands the Bukkit
		// ancestry (Folia can host lower-rung plugins), but the install/compat
		// layer enforces the opt-in separately.
		PolicyEdges: []RegistryEdge{
			{
				TargetNodeID: types.RuntimeNodePaper,
				Kind:         types.EdgeExtends,
			},
		},
	},
	{
		NodeID:       types.RuntimeNodeLeaves,
		Role:         types.RuntimeRolePluginCore,
		RiskLevel:    types.RiskNone,
		Capabilities: types.CapabilityPurpurPlugins.Populate(),
		// Leaves is a Purpur fork (LeavesMC/Leaves) and inherits the full Purpur
		// API surface plus its own additions. Declared rung is Purpur; Populate()
		// expands the Bukkit/Spigot/Paper ancestry.
		PolicyEdges: []RegistryEdge{
			{
				TargetNodeID: types.RuntimeNodePaper,
				Kind:         types.EdgeExtends,
			},
		},
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
		Capabilities: append(
			types.CapabilityForgeMods.Populate(),
			types.CapabilitySpigotPlugins.Populate()...,
		),
		// Arclight implements the Bukkit/Spigot tier (not Paper) per its FAQ.
		// Paper API support is in progress as of 2025.
	},
	{
		NodeID: types.RuntimeNodeCatServer,
		Role:   types.RuntimeRoleHybrid,
		Capabilities: append(
			types.CapabilityForgeMods.Populate(),
			types.CapabilitySpigotPlugins.Populate()...,
		),
		// CatServer implements the Bukkit/Spigot tier (not Paper).
	},
	{
		NodeID: types.RuntimeNodeYouer,
		Role:   types.RuntimeRoleHybrid,
		Capabilities: append(
			types.CapabilityNeoforgeMods.Populate(),
			types.CapabilityPurpurPlugins.Populate()...,
		),
		// Youer integrates the full Bukkit-family chain (Bukkit/CraftBukkit/Spigot/
		// Paper/Purpur) on top of NeoForge. Anchoring to Paper captures the highest
		// rung of plugin API support and distinguishes Youer from Arclight/CatServer,
		// which only implement the Bukkit/Spigot tier.
		PolicyEdges: []RegistryEdge{
			{
				TargetNodeID: types.RuntimeNodePaper,
				Kind:         types.EdgeExtends,
			},
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
		// RuntimeNode is still homogeneous, so adapted environments are folded into
		// the adapter's capabilities instead of being expanded as virtual nodes.
		Capabilities: []types.RuntimeCapability{
			types.CapabilityFabricMods,
		},
	},
	{
		NodeID:    types.RuntimeNodeKilt,
		Role:      types.RuntimeRoleBridge,
		RiskLevel: types.RiskHigh,
	},
}
