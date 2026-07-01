package workspace

import (
	"github.com/mclucy/lucy/types"
)

func ecosystemsForCapability(c types.RuntimeCapability) []types.Ecosystem {
	switch c {
	case types.CapabilityFabricLoader:
		return []types.Ecosystem{types.EcoFabric}
	case types.CapabilityForge:
		return []types.Ecosystem{types.EcoForge}
	case types.CapabilityNeoforge:
		return []types.Ecosystem{types.EcoNeoforge}
	case types.CapabilityBukkitAPI, types.CapabilitySpigotAPI:
		return []types.Ecosystem{types.EcoBukkit}
	case types.CapabilityPaperAPI,
		types.CapabilityPurpurAPI,
		types.CapabilityFoliaAPI:
		return []types.Ecosystem{types.EcoPaper}
	case types.CapabilityMcdr:
		return []types.Ecosystem{types.EcoMcdr}
	default:
		return nil
	}
}

func projectEcosystemsFromTopology(
	topology *types.ServerTopology,
) (native, hosted []types.Ecosystem, identities []types.VersionedPackageRef) {
	if topology == nil || !topology.Resolved() {
		return nil, nil, nil
	}

	hostedNodeIDs := make(map[types.RuntimeNodeID]struct{})
	for _, edge := range topology.Edges {
		if edge.Verb != types.EdgeHosts {
			continue
		}
		hostedNodeIDs[edge.To] = struct{}{}
	}

	nativeSet := map[types.Ecosystem]struct{}{}
	hostedSet := map[types.Ecosystem]struct{}{}

	for _, node := range topology.Nodes {
		_, onHostedPath := hostedNodeIDs[node.ID]
		for _, cap := range node.Capabilities {
			for _, eco := range ecosystemsForCapability(cap) {
				if onHostedPath {
					hostedSet[eco] = struct{}{}
				} else {
					nativeSet[eco] = struct{}{}
				}
			}
		}
	}

	native = ecosystemsFromSet(nativeSet)
	hosted = ecosystemsFromSet(hostedSet)
	identities = topology.AllIdentities()
	return native, hosted, identities
}

func ecosystemsFromSet(set map[types.Ecosystem]struct{}) []types.Ecosystem {
	if len(set) == 0 {
		return nil
	}
	out := make([]types.Ecosystem, 0, len(set))
	for eco := range set {
		out = append(out, eco)
	}
	return out
}

func SyncServerInstanceFromTopology(exec *ServerInstance) {
	if exec == nil {
		return
	}
	native, hosted, identities := projectEcosystemsFromTopology(exec.topology)
	exec.primaryEcosystems = native
	exec.secondaryEcosystems = hosted
	exec.Cores = mergeCoreRefs(exec.Cores, identities)
}

func mergeCoreRefs(
	existing, fromTopology []types.VersionedPackageRef,
) []types.VersionedPackageRef {
	seen := map[string]struct{}{}
	var out []types.VersionedPackageRef
	add := func(ref types.VersionedPackageRef) {
		key := ref.StringFull()
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, ref)
	}
	for _, ref := range existing {
		add(ref)
	}
	for _, ref := range fromTopology {
		add(ref)
	}
	return out
}

func upsertMinecraftCore(
	cores []types.VersionedPackageRef,
	gameVersion types.BareVersion,
) []types.VersionedPackageRef {
	if gameVersion == types.VersionUnknown ||
		gameVersion == types.VersionNone ||
		gameVersion == "" {
		return cores
	}
	mc := types.VersionedPackageRef{
		PackageRef: types.PackageRef{
			Eco:  types.EcoMinecraft,
			Name: "minecraft",
		},
		Version: gameVersion,
	}
	return mergeCoreRefs(cores, []types.VersionedPackageRef{mc})
}
