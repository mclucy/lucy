package workspace

import (
	"path/filepath"

	"github.com/mclucy/lucy/types"
)

func finalizeProbedRuntime(
	runtime *ServerInstance,
	packages []types.DiscoveredPackage,
) []types.DiscoveredPackage {
	EnrichTopologyFromPackages(runtime, packages)
	ensureRuntimeTopology(runtime)
	return packagesWithRuntimeIdentities(packages, runtime)
}

func ensureRuntimeTopology(runtime *ServerInstance) {
	if runtime == nil || runtime.topology != nil {
		return
	}

	runtime.topology = &types.ServerTopology{}
	SyncServerInstanceFromTopology(runtime)
}

func packagesWithRuntimeIdentities(
	packages []types.DiscoveredPackage,
	runtime *ServerInstance,
) []types.DiscoveredPackage {
	if runtime == nil || !runtime.IsValid() {
		return packages
	}

	idx := NewPackageIndex()
	idx.Merge(packages)
	for _, rid := range runtime.topology.AllIdentities() {
		if rid.Eco == types.EcoAny {
			continue
		}
		idx.Add(types.DiscoveredPackage{Id: rid})
	}

	return idx.Packages()
}

func packageSearchPaths(
	runtime *ServerInstance,
	workingDirectory string,
) []string {
	if runtime == nil {
		return nil
	}

	return packageSearchPathsForTopology(runtime.topology, workingDirectory)
}

func packageSearchPathsForTopology(
	topology *types.ServerTopology,
	workingDirectory string,
) (paths []string) {
	if topology == nil || !topology.Resolved() {
		return nil
	}

	if topology.HasCapability(types.CapabilityFabricLoader) ||
		topology.HasCapability(types.CapabilityForge) ||
		topology.HasCapability(types.CapabilityNeoforge) {
		paths = append(paths, filepath.Join(workingDirectory, "mods"))
	}
	if topology.HasCapability(types.CapabilityBukkitAPI) {
		paths = append(paths, filepath.Join(workingDirectory, "plugins"))
	}

	return paths
}
