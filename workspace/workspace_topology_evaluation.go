// Package workspace is a pure policy layer
// These evaluators are deterministic and side-effect free. They take topology
// values as input and return compatibility verdicts.
//
// No file I/O, no network calls, no logging, no panic.
package workspace

import (
	"fmt"

	"github.com/mclucy/lucy/types"
)

// EvaluateCompatibility evaluates whether a server runtime (described by topology)
// can support the requested ecosystem. Verdict encodes direct support, indirect/hosted
// support, incompatibility, or unresolved topology. Indirect support is reported as
// CompatDegraded, while runtime risk remains a node-level topology concern. Never
// returns nil - always returns a deterministic result.
func EvaluateCompatibility(
	topology *types.ServerTopology,
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

// CapabilityForEcosystem maps a package's Platform identity to the RuntimeCapability
// it requires in the host server's topology. Returns the empty RuntimeCapability when
// no mapping exists, including for topology-only/proxy platforms (velocity,
// bungeecord, waterfall, sponge) and unknown platforms.
func CapabilityForEcosystem(p types.Ecosystem) types.RuntimeCapability {
	switch p {
	case types.EcoFabric:
		return types.CapabilityFabricLoader
	case types.EcoForge:
		return types.CapabilityForge
	case types.EcoNeoforge:
		return types.CapabilityNeoforge
	case types.EcoBukkit:
		return types.CapabilityBukkitAPI
	case types.EcoPaper:
		return types.CapabilityPaperAPI
	case types.EcoMcdr:
		return types.CapabilityMcdr
	default:
		return ""
	}
}
