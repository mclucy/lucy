package cli

import (
	"strings"

	"github.com/mclucy/lucy/tui/style"
	"github.com/mclucy/lucy/types"
)

// RuntimeRoleLabel returns the display label for a runtime role.
func RuntimeRoleLabel(role types.RuntimeRole) string {
	switch role {
	case types.RuntimeRoleModLoader:
		return "Mod loader"
	case types.RuntimeRolePluginCore:
		return "Plugin core"
	case types.RuntimeRoleHybrid:
		return "Hybrid"
	case types.RuntimeRoleProxy:
		return "Proxy"
	case types.RuntimeRoleBridge:
		return "Bridge"
	case types.RuntimeRoleProtocolBridge:
		return "Protocol bridge"
	case types.RuntimeRoleVanilla:
		return "Vanilla"
	default:
		return ""
	}
}

// RuntimeNodeLabel returns the display label for a runtime node.
func RuntimeNodeLabel(id types.RuntimeNodeID) string {
	switch id {
	case types.RuntimeNodeMinecraft:
		return "Vanilla"
	case types.RuntimeNodeFabric:
		return "Fabric"
	case types.RuntimeNodeForge:
		return "Forge"
	case types.RuntimeNodeNeoforge:
		return "NeoForge"
	case types.RuntimeNodeMCDR:
		return "MCDR"
	case types.RuntimeNodePaper:
		return "Paper"
	case types.RuntimeNodeSpigot:
		return "Spigot"
	case types.RuntimeNodeBukkit:
		return "Bukkit"
	case types.RuntimeNodeFolia:
		return "Folia"
	case types.RuntimeNodeLeaves:
		return "Leaves"
	case types.RuntimeNodeSponge:
		return "Sponge"
	case types.RuntimeNodeArclight:
		return "Arclight"
	case types.RuntimeNodeCatServer:
		return "CatServer"
	case types.RuntimeNodeYouer:
		return "Youer"
	case types.RuntimeNodeVelocity:
		return "Velocity"
	case types.RuntimeNodeBungeecord:
		return "BungeeCord"
	case types.RuntimeNodeWaterfall:
		return "Waterfall"
	case types.RuntimeNodeGeyserStandalone:
		return "Geyser Standalone"
	case types.RuntimeNodeGeyser:
		return "Geyser"
	case types.RuntimeNodeConnector:
		return "Sinytra Connector"
	case types.RuntimeNodeKilt:
		return "Kilt"
	default:
		return style.Capitalize(
			strings.ReplaceAll(
				strings.ReplaceAll(
					string(id),
					"-",
					" ",
				), "_", " ",
			),
		)
	}
}
