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

	hostedTargets := make(map[types.RuntimeNodeID]types.RuntimeRiskLevel, len(topology.Edges))
	for _, edge := range topology.Edges {
		if edge.Verb != types.EdgeHosts {
			continue
		}

		targetNode, ok := topology.FindNode(edge.To)
		if !ok || !targetNode.HasCapability(requiredCapability) {
			continue
		}

		hostedRisk := max(edge.Risk, targetNode.RiskLevel)
		hostedTargets[edge.To] = max(hostedTargets[edge.To], hostedRisk)
	}

	for _, node := range topology.Nodes {
		if _, isHostedTarget := hostedTargets[node.ID]; isHostedTarget {
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

	for _, hostedRisk := range hostedTargets {
		verdict := types.CompatCompatible
		if hostedRisk >= types.RiskMedium {
			verdict = types.CompatDegraded
		}

		return types.CompatResult{
			Verdict:   verdict,
			Reason:    "hosted_layer_capability_match",
			Detail:    fmt.Sprintf("Compatibility provided by a hosted layer with %s support (risk: %d).", requiredCapability, hostedRisk),
			RiskLevel: hostedRisk,
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
