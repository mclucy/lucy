// Package probe provides functionality to gather and manage server information
// for a Minecraft server. It includes methods to retrieve server configuration,
// mod list, executable information, and other relevant details. The package
// utilizes memoization to avoid redundant calculations and resolve any data
// dependencies issues. Therefore, all probe functions are 100% concurrent-safe.
//
// The main exposed function is ServerInfo, which returns a comprehensive
// ServerInfo struct containing all the gathered information. To avoid side
// effects, the ServerInfo struct is returned as a copy, rather than reference.
package probe

import (
	"fmt"

	"github.com/mclucy/lucy/types"
)

// PURE POLICY LAYER: These evaluators are deterministic and side-effect free.
// They take topology values as input and return compatibility verdicts.
// No file I/O, no network calls, no logging, no panic.
//
// EvaluateCompatibility evaluates whether a server runtime (described by topology)
// can host packages of the given capability/ecosystem.
// Returns a CompatResult with verdict, reason code, and risk level.
// Never returns nil - always returns a deterministic result.
func EvaluateCompatibility(topology *types.RuntimeTopology, requiredCapability types.RuntimeCapability) types.CompatResult {
	if topology == nil || !topology.Resolved() {
		return types.CompatResult{
			Verdict:   types.CompatUnresolved,
			Reason:    "topology_unresolved",
			Detail:    "Server runtime topology has not been probed or could not be determined.",
			RiskLevel: types.RiskMedium,
		}
	}

	// Collect bridge-target node IDs so we can exclude them from the direct
	// capability scan. Nodes that are only reachable via a bridge edge must
	// be evaluated through the bridge path (which accounts for risk level).
	bridgeTargets := make(map[types.RuntimeNodeID]struct{}, len(topology.Edges))
	for _, edge := range topology.Edges {
		if edge.Kind == types.EdgeBridges {
			bridgeTargets[edge.To] = struct{}{}
		}
	}

	for _, node := range topology.Nodes {
		if _, isBridgeTarget := bridgeTargets[node.ID]; isBridgeTarget {
			continue
		}
		if node.HasCapability(requiredCapability) {
			return types.CompatResult{
				Verdict:   types.CompatCompatible,
				Reason:    "direct_capability_match",
				Detail:    fmt.Sprintf("Runtime has direct support for %s.", requiredCapability),
				RiskLevel: types.RiskNone,
			}
		}
	}

	for _, edge := range topology.Edges {
		if edge.Kind != types.EdgeBridges {
			continue
		}

		targetNode, ok := topology.FindNode(edge.To)
		if !ok || !targetNode.HasCapability(requiredCapability) {
			continue
		}

		verdict := types.CompatCompatible
		if edge.Risk >= types.RiskMedium {
			verdict = types.CompatDegraded
		}

		return types.CompatResult{
			Verdict:   verdict,
			Reason:    "bridge_compatibility",
			Detail:    fmt.Sprintf("Compatibility provided via bridge layer (risk: %d).", edge.Risk),
			RiskLevel: edge.Risk,
		}
	}

	return types.CompatResult{
		Verdict:   types.CompatIncompatible,
		Reason:    "no_capability_match",
		Detail:    fmt.Sprintf("Runtime does not support %s.", requiredCapability),
		RiskLevel: types.RiskNone,
	}
}

// CapabilityForPlatform maps a package's Platform identity to the RuntimeCapability
// it requires in the host server's topology. Returns empty string if no mapping exists.
func CapabilityForPlatform(p types.Platform) types.RuntimeCapability {
	switch p {
	case types.PlatformFabric:
		return types.CapabilityFabricMods
	case types.PlatformForge:
		return types.CapabilityForgeMods
	case types.PlatformNeoforge:
		return types.CapabilityNeoforgeMods
	case types.Platform("bukkit"), types.Platform("paper"), types.Platform("spigot"), types.Platform("folia"), types.Platform("leaves"):
		return types.CapabilityBukkitPlugins
	case types.Platform("velocity"):
		return types.CapabilityVelocityPlugins
	case types.Platform("bungeecord"), types.Platform("bungee"), types.Platform("waterfall"):
		return types.CapabilityBungeecordPlugins
	case types.PlatformMCDR:
		return types.CapabilityMCDRPlugins
	case types.Platform("sponge"):
		return types.CapabilitySpongePlugins
	default:
		return ""
	}
}
