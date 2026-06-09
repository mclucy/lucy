package probe

import (
	"strings"

	"github.com/mclucy/lucy/types"
)

// probe_topology_registry_data.go contains procedural topology lookup helpers,
// not the declarative topology source-of-truth.

var normalizedRuntimeIDByName = map[string]types.RuntimeNodeID{
	"minecraft":         types.RuntimeNodeMinecraft,
	"vanilla":           types.RuntimeNodeMinecraft,
	"fabric":            types.RuntimeNodeFabric,
	"fabric server":     types.RuntimeNodeFabric,
	"forge":             types.RuntimeNodeForge,
	"forge server":      types.RuntimeNodeForge,
	"neoforge":          types.RuntimeNodeNeoforge,
	"neoforge server":   types.RuntimeNodeNeoforge,
	"mcdr":              types.RuntimeNodeMCDR,
	"mcdr plugin":       types.RuntimeNodeMCDR,
	"paper":             types.RuntimeNodePaper,
	"spigot":            types.RuntimeNodeSpigot,
	"paper-fork":        types.RuntimeNodePaperFork,
	"craftbukkit":       types.RuntimeNodeCraftBukkit,
	"bukkit":            types.RuntimeNodeBukkit,
	"folia":             types.RuntimeNodeFolia,
	"leaves":            types.RuntimeNodeLeaves,
	"velocity":          types.RuntimeNodeVelocity,
	"bungeecord":        types.RuntimeNodeBungeecord,
	"bungee":            types.RuntimeNodeBungeecord,
	"waterfall":         types.RuntimeNodeWaterfall,
	"sponge":            types.RuntimeNodeSponge,
	"arclight":          types.RuntimeNodeArclight,
	"kilt":              types.RuntimeNodeKilt,
	"geyser":            types.RuntimeNodeGeyser,
	"geyser standalone": types.RuntimeNodeGeyserStandalone,
}

func NormalizeRuntimeID(name string) types.RuntimeNodeID {
	normalized := strings.TrimSpace(strings.ToLower(name))
	if normalized == "" {
		return types.RuntimeNodeUnknown
	}

	id, ok := normalizedRuntimeIDByName[normalized]
	if !ok {
		return types.RuntimeNodeUnknown
	}

	return id
}
