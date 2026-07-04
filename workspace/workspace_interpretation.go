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
	if runtime != nil {
		runtime.Cores = enrichCoresFromPackages(runtime.Cores, packages)
		runtime.RefreshPrimaryCore()
	}
	out := packagesWithRuntimeIdentities(packages, runtime)
	if runtime != nil {
		runtime.Packages = out
	}
	return out
}

func ensureRuntimeTopology(runtime *ServerInstance) {
	if runtime == nil || runtime.topology != nil {
		return
	}

	runtime.topology = &types.ServerTopology{}
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
	for _, rid := range runtime.Cores {
		if rid.Eco == types.EcoUnspecified {
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

	return packageSearchPathsForServer(runtime, workingDirectory)
}

func packageSearchPathsForServer(
	server *ServerInstance,
	workingDirectory string,
) (paths []string) {
	if server == nil || !server.Analyzable() {
		return nil
	}

	for _, eco := range server.PrimaryEcosystem() {
		switch eco {
		case types.EcoFabric, types.EcoForge, types.EcoNeoforge:
			paths = append(paths, filepath.Join(workingDirectory, "mods"))
		case types.EcoBukkit, types.EcoPaper:
			paths = append(paths, filepath.Join(workingDirectory, "plugins"))
		}
	}
	return paths
}
