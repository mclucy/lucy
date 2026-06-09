package probe

import (
	"path"

	"github.com/mclucy/lucy/types"
)

func finalizeProbedRuntime(
	runtime *ServerRuntime,
	packages []types.Package,
) []types.Package {
	EnrichTopologyFromPackages(runtime, packages)
	ensureRuntimeTopology(runtime)
	return packagesWithRuntimeIdentities(packages, runtime)
}

func ensureRuntimeTopology(runtime *ServerRuntime) {
	if runtime == nil || runtime.Topology != nil {
		return
	}

	runtime.Topology = &types.RuntimeTopology{}
}

func packagesWithRuntimeIdentities(
	packages []types.Package,
	runtime *ServerRuntime,
) []types.Package {
	if runtime == nil || !runtime.IsValid() {
		return packages
	}

	idx := NewPackageIndex()
	idx.Merge(packages)
	for _, rid := range runtime.RuntimeIdentities {
		if rid.Platform == types.PlatformAny {
			continue
		}
		idx.Add(types.Package{Id: rid})
	}

	return idx.Packages()
}

func packageSearchPaths(
	runtime *ServerRuntime,
	workingDirectory string,
) []string {
	if runtime == nil {
		return nil
	}

	return packageSearchPathsForTopology(runtime.Topology, workingDirectory)
}

func packageSearchPathsForTopology(
	topology *types.RuntimeTopology,
	workingDirectory string,
) (paths []string) {
	if topology == nil || !topology.Resolved() {
		return nil
	}

	if topology.HasCapability(types.CapabilityFabricMods) ||
		topology.HasCapability(types.CapabilityForgeMods) ||
		topology.HasCapability(types.CapabilityNeoforgeMods) {
		paths = append(paths, path.Join(workingDirectory, "mods"))
	}
	if topology.HasCapability(types.CapabilityBukkitPlugins) {
		paths = append(paths, path.Join(workingDirectory, "plugins"))
	}

	return paths
}
