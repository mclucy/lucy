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
// can support the requested ecosystem. Verdict encodes direct support, indirect/hosted
// support, incompatibility, or unresolved topology. Indirect support is reported as
// CompatDegraded, while runtime risk remains a node-level topology concern. Never
// returns nil - always returns a deterministic result.
func EvaluateCompatibility(
	topology *types.RuntimeTopology,
	requiredCapability types.RuntimeCapability,
) types.CompatResult {
	if topology == nil || !topology.Resolved() {
		return types.CompatResult{
			Verdict: types.CompatUnresolved,
			Reason:  "topology_unresolved",
			Detail:  "Server runtime topology has not been probed or could not be determined.",
		}
	}

	// Collect nodes reachable only via EdgeHosts (indirect/hosted paths).
	hostedTargets := make(map[types.RuntimeNodeID]struct{}, len(topology.Edges))
	for _, edge := range topology.Edges {
		if edge.Verb != types.EdgeHosts {
			continue
		}

		targetNode, ok := topology.FindNode(edge.To)
		if !ok || !targetNode.HasCapability(requiredCapability) {
			continue
		}

		hostedTargets[edge.To] = struct{}{}
	}

	// Direct capability match (not via hosted path).
	for _, node := range topology.Nodes {
		if _, isHostedTarget := hostedTargets[node.ID]; isHostedTarget {
			continue
		}

		if node.HasCapability(requiredCapability) {
			return types.CompatResult{
				Verdict: types.CompatCompatible,
				Reason:  "direct_capability_match",
				Detail: fmt.Sprintf(
					"Runtime has direct support for %s.",
					requiredCapability,
				),
			}
		}
	}

	// Indirect/hosted capability match — always degraded regardless of node risk.
	if len(hostedTargets) > 0 {
		return types.CompatResult{
			Verdict: types.CompatDegraded,
			Reason:  "indirect_capability_match",
			Detail: fmt.Sprintf(
				"Support for %s is available through a hosted or indirect runtime path.",
				requiredCapability,
			),
		}
	}

	return types.CompatResult{
		Verdict: types.CompatIncompatible,
		Reason:  "no_capability_match",
		Detail: fmt.Sprintf(
			"Runtime does not support %s.",
			requiredCapability,
		),
	}
}

// CapabilityForPlatform maps a package's Platform identity to the RuntimeCapability
// it requires in the host server's topology. Returns empty string if no mapping exists.
func CapabilityForPlatform(p types.PlatformId) types.RuntimeCapability {
	switch p {
	case types.PlatformFabric:
		return types.CapabilityFabricMods
	case types.PlatformForge:
		return types.CapabilityForgeMods
	case types.PlatformNeoforge:
		return types.CapabilityNeoforgeMods
	case types.PlatformId("bukkit"), types.PlatformId("paper"), types.PlatformId("spigot"), types.PlatformId("folia"), types.PlatformId("leaves"):
		return types.CapabilityBukkitPlugins
	case types.PlatformId("velocity"):
		return types.CapabilityVelocityPlugins
	case types.PlatformId("bungeecord"), types.PlatformId("bungee"), types.PlatformId("waterfall"):
		return types.CapabilityBungeecordPlugins
	case types.PlatformMCDR:
		return types.CapabilityMCDRPlugins
	case types.PlatformId("sponge"):
		return types.CapabilitySpongePlugins
	default:
		return ""
	}
}
