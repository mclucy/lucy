package routing

import (
	"github.com/mclucy/lucy/types"
)

func ecosystemForCapability(caps ...types.RuntimeCapability) []types.Ecosystem {
	seen := map[types.Ecosystem]struct{}{}
	var out []types.Ecosystem
	for _, cap := range caps {
		for _, eco := range ecosystemForSingleCapability(cap) {
			if _, ok := seen[eco]; ok {
				continue
			}
			seen[eco] = struct{}{}
			out = append(out, eco)
		}
	}
	return out
}

func ecosystemForSingleCapability(cap types.RuntimeCapability) []types.Ecosystem {
	switch cap {
	case types.CapabilityBukkitAPI:
		return []types.Ecosystem{types.EcoBukkit}
	case types.CapabilitySpigotAPI:
		return []types.Ecosystem{types.EcoBukkit}
	case types.CapabilityPaperAPI, types.CapabilityPurpurAPI, types.CapabilityFoliaAPI:
		return []types.Ecosystem{types.EcoPaper}
	case types.CapabilityFabricLoader:
		return []types.Ecosystem{types.EcoFabric}
	case types.CapabilityForge:
		return []types.Ecosystem{types.EcoForge}
	case types.CapabilityNeoforge:
		return []types.Ecosystem{types.EcoNeoforge}
	case types.CapabilityMcdr:
		return []types.Ecosystem{types.EcoMcdr}
	case types.CapabilityVelocity:
		return []types.Ecosystem{types.EcoVelocity}
	case types.CapabilityBungeecord:
		return []types.Ecosystem{types.EcoBungeecord}
	case types.CapabilitySpongeAPI:
		return []types.Ecosystem{types.EcoSponge}
	default:
		return nil
	}
}

func ecosystemsFromTopology(topo *types.ServerTopology) []types.Ecosystem {
	if topo == nil {
		return nil
	}
	seen := map[types.Ecosystem]struct{}{}
	var out []types.Ecosystem
	for _, node := range topo.Nodes {
		for _, cap := range node.Capabilities {
			for _, eco := range ecosystemForSingleCapability(cap) {
				if _, ok := seen[eco]; ok {
					continue
				}
				seen[eco] = struct{}{}
				out = append(out, eco)
			}
		}
	}
	return out
}
