package probe

import (
	"strings"

	"github.com/mclucy/lucy/types"
)

var normalizedRuntimeIDByName = map[string]types.RuntimeNodeID{
	"minecraft":       RuntimeNodeMinecraft,
	"vanilla":         RuntimeNodeMinecraft,
	"fabric":          RuntimeNodeFabric,
	"fabric server":   RuntimeNodeFabric,
	"forge":           RuntimeNodeForge,
	"forge server":    RuntimeNodeForge,
	"neoforge":        RuntimeNodeNeoforge,
	"neoforge server": RuntimeNodeNeoforge,
	"mcdr":            RuntimeNodeMCDR,
	"mcdr plugin":     RuntimeNodeMCDR,
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
