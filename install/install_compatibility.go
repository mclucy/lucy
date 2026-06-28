package install

import (
	"slices"

	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/workspace"
)

func makeWorkspaceCompatibilityFunc(
	serverInfo workspace.Workspace,
) upstream.CompatibilityFunc {
	gameVersion := types.VersionUnknown
	if serverInfo.Runtime != nil {
		gameVersion = serverInfo.Runtime.GameVersion
	}
	loader := serverInfo.DerivedModLoader()
	if loader == types.EcoBare || loader == types.EcoAny {
		loader = requestedEcosystemFromTopology(serverInfo.Topology)
	}

	return func(candidate upstream.VersionCandidate) bool {
		return candidateSupportsGameVersion(candidate, gameVersion) &&
			candidateSupportsLoader(candidate, loader)
	}
}

func candidateSupportsGameVersion(
	candidate upstream.VersionCandidate,
	gameVersion types.BareVersion,
) bool {
	if gameVersion == "" || gameVersion == types.VersionUnknown || len(candidate.GameVersions) == 0 {
		return true
	}
	return slices.Contains(candidate.GameVersions, gameVersion)
}

func candidateSupportsLoader(
	candidate upstream.VersionCandidate,
	loader types.Ecosystem,
) bool {
	if loader == types.EcoAny || loader == types.EcoBare || len(candidate.Loaders) == 0 {
		return true
	}
	for _, candidateLoader := range candidate.Loaders {
		if candidateLoader.Satisfy(loader) {
			return true
		}
	}
	return false
}

func requestedEcosystemFromTopology(topology *types.RuntimeTopology) types.Ecosystem {
	if topology == nil {
		return types.EcoBare
	}
	switch {
	case topology.HasCapability(types.CapabilityFabricMods):
		return types.EcoFabric
	case topology.HasCapability(types.CapabilityNeoforgeMods):
		return types.EcoNeoforge
	case topology.HasCapability(types.CapabilityForgeMods):
		return types.EcoForge
	case topology.HasCapability(types.CapabilityBukkitPlugins):
		return types.EcoBukkit
	default:
		return types.EcoBare
	}
}
