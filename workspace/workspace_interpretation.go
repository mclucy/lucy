package workspace

import (
	"path/filepath"

	"github.com/mclucy/lucy/types"
)

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
	if server == nil || !server.IsValid() {
		return nil
	}

	for _, offer := range server.EffectiveEcosystems() {
		if offer.Compatibility != types.CompatCompatible {
			continue
		}

		var path string
		switch offer.Ecosystem {
		case types.EcoFabric, types.EcoForge, types.EcoNeoforge:
			path = filepath.Join(workingDirectory, "mods")
		case types.EcoBukkit, types.EcoPaper:
			path = filepath.Join(workingDirectory, "plugins")
		default:
			continue
		}

		duplicate := false
		for _, existing := range paths {
			if existing == path {
				duplicate = true
				break
			}
		}
		if !duplicate {
			paths = append(paths, path)
		}
	}
	return paths
}
