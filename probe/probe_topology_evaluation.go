package probe

import (
	"fmt"

	"github.com/mclucy/lucy/types"
)

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

	for _, node := range topology.Nodes {
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
	case types.PlatformMCDR:
		return types.CapabilityMCDRPlugins
	default:
		return ""
	}
}
