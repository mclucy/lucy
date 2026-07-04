package workspace

import (
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/workspace/internal/detector"
)

func hostedRuntimeNodeIDs(topology *types.ServerTopology) map[types.RuntimeNodeID]struct{} {
	out := map[types.RuntimeNodeID]struct{}{}
	if topology == nil {
		return out
	}
	for _, edge := range topology.Edges {
		if edge.Verb != types.EdgeHosts {
			continue
		}
		out[edge.To] = struct{}{}
	}
	return out
}

func coreRefFromRuntimeNodeID(id types.RuntimeNodeID) (types.VersionedPackageRef, bool) {
	if id == types.RuntimeNodeUnknown || id == "" {
		return types.VersionedPackageRef{}, false
	}
	candidates := []types.PackageRef{
		{Eco: types.EcoUnspecified, Name: types.BarePackageName(id)},
		{Eco: types.DeclaredModdingEcosystemForNode(id), Name: types.BarePackageName(id)},
	}
	for _, ref := range candidates {
		if _, ok := types.LookupCore(ref); ok {
			return types.VersionedPackageRef{
				PackageRef: ref,
				Version:    types.VersionUnknown,
			}, true
		}
	}
	return types.VersionedPackageRef{}, false
}

func coreRefsFromTopology(topology *types.ServerTopology) []types.VersionedPackageRef {
	if topology == nil || !topology.Resolved() {
		return nil
	}
	hosted := hostedRuntimeNodeIDs(topology)
	var out []types.VersionedPackageRef
	for _, node := range topology.Nodes {
		if _, onHosted := hosted[node.ID]; onHosted {
			continue
		}
		if ref, ok := coreRefFromRuntimeNodeID(node.ID); ok {
			out = append(out, ref)
		}
	}
	return mergeCoreRefs(nil, append(out, topology.AllIdentities()...))
}

func coresFromExecutableEvidence(
	evidence *detector.ExecutableEvidence,
	topology *types.ServerTopology,
) []types.VersionedPackageRef {
	if evidence == nil {
		return nil
	}
	cores := mergeCoreRefs(nil, evidence.RuntimeIdentities)
	if topology != nil {
		cores = mergeCoreRefs(cores, coreRefsFromTopology(topology))
	}
	return cores
}

func enrichCoresFromPackages(
	existing []types.VersionedPackageRef,
	packages []types.DiscoveredPackage,
) []types.VersionedPackageRef {
	cores := mergeCoreRefs(nil, existing)
	for _, pkg := range packages {
		if _, ok := types.LookupCore(pkg.Id.PackageRef); ok {
			cores = mergeCoreRefs(cores, []types.VersionedPackageRef{pkg.Id})
		}
	}
	return cores
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
