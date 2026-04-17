package probe

import (
	"strings"

	"github.com/mclucy/lucy/types"
)

// probe_topology_registry_data.go contains procedural topology lookup helpers,
// not the declarative topology source-of-truth.

var normalizedRuntimeIDByName = map[string]types.RuntimeNodeID{
	"minecraft":         RuntimeNodeMinecraft,
	"vanilla":           RuntimeNodeMinecraft,
	"fabric":            RuntimeNodeFabric,
	"fabric server":     RuntimeNodeFabric,
	"forge":             RuntimeNodeForge,
	"forge server":      RuntimeNodeForge,
	"neoforge":          RuntimeNodeNeoforge,
	"neoforge server":   RuntimeNodeNeoforge,
	"mcdr":              RuntimeNodeMCDR,
	"mcdr plugin":       RuntimeNodeMCDR,
	"paper":             RuntimeNodePaper,
	"spigot":            RuntimeNodeSpigot,
	"paper-fork":        RuntimeNodePaperFork,
	"craftbukkit":       RuntimeNodeCraftBukkit,
	"bukkit":            RuntimeNodeBukkit,
	"folia":             RuntimeNodeFolia,
	"leaves":            RuntimeNodeLeaves,
	"velocity":          RuntimeNodeVelocity,
	"bungeecord":        RuntimeNodeBungeecord,
	"bungee":            RuntimeNodeBungeecord,
	"waterfall":         RuntimeNodeWaterfall,
	"sponge":            RuntimeNodeSponge,
	"arclight":          RuntimeNodeArclight,
	"kilt":              RuntimeNodeKilt,
	"geyser":            RuntimeNodeGeyser,
	"geyser standalone": RuntimeNodeGeyserStandalone,
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
