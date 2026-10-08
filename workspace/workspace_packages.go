package workspace

import (
	"strings"
	"sync"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/upstream/providers/curseforge"
	"github.com/mclucy/lucy/upstream/providers/modrinth"
)

// resolveUpstream resolves the upstream of an artifact by hash,
// Modrinth first.
func resolveUpstream(
	path string,
) (types.VersionedPackageRef, bool) {
	mappers := []upstream.ArtifactMapSource{modrinth.Provider}
	if curseforge.Enabled() {
		mappers = append(mappers, curseforge.Provider)
	}

	for _, mapper := range mappers {
		ref, _, ok, err := mapper.PackageByHash(
			artifact.File{Path: path},
		)
		if err != nil || !ok || ref.Name == "" {
			continue
		}
		return ref, true
	}
	return types.VersionedPackageRef{}, false
}

// discoverPackages inventories the packages under searchPaths and
// mcdrPluginDirs, when the MCDR environment exists. Deduplication and
// local-path enrichment follow the PackageIndex.Add policy.
func discoverPackages(
	searchPaths []string,
	mcdrPluginDirs []string,
) []types.DiscoveredPackage {
	idx := NewPackageIndex()
	var mu sync.Mutex

	for _, searchPath := range searchPaths {
		jarFiles, err := findJar(searchPath)
		if err != nil {
			log.Warn(err)
			log.Info("cannot read the mod directory")
			continue
		}

		var wg sync.WaitGroup
		for _, jarPath := range jarFiles {
			wg.Add(1)
			go func(path string) {
				defer wg.Done()

				analyzed, err := artifact.Analyze(path)
				upstreamRef, hit := resolveUpstream(path)

				if err != nil || len(analyzed) == 0 {
					if !hit {
						return
					}
					mu.Lock()
					idx.Add(discoveredFromUpstream(path, upstreamRef))
					mu.Unlock()
					return
				}
				pkgs := artifactInfoToDiscoveredPackage(analyzed)

				mu.Lock()
				idx.Merge(pkgs)
				mu.Unlock()
			}(jarPath)
		}
		wg.Wait()
	}

	for _, dir := range mcdrPluginDirs {
		pluginFiles, err := findFileWithExt([]string{dir}, ".pyz", ".mcdr")
		if err != nil {
			log.Warn(err)
			log.Info("cannot read the MCDR plugin directory")
			continue
		}
		for _, pluginFile := range pluginFiles {
			analyzed, err := artifact.Analyze(pluginFile)
			resolveUpstream(pluginFile)
			if err == nil && len(analyzed) > 0 {
				pkgs := artifactInfoToDiscoveredPackage(analyzed)
				idx.Merge(pkgs)
			}
		}
	}

	return idx.Packages()
}

func artifactInfoToDiscoveredPackage(infos []artifact.Info) []types.DiscoveredPackage {
	if len(infos) == 0 {
		return nil
	}
	pkgs := make([]types.DiscoveredPackage, 0, len(infos))
	for _, info := range infos {
		pkg := types.DiscoveredPackage{
			Id: types.VersionedPackageRef{
				PackageRef: info.Ref.PackageRef,
				Eco:        info.Ref.Eco,
				Version:    info.Version,
			},
			Path: info.FilePath,
		}
		if len(info.Dependencies) > 0 {
			deps := make([]types.Dependency, 0, len(info.Dependencies))
			for _, dep := range info.Dependencies {
				deps = append(deps, types.Dependency{
					Id: types.VersionedPackageRef{
						PackageRef: dep.Ref.PackageRef,
						Eco:        dep.Ref.Eco,
					},
					Constraint: dep.Constraint,
					Mandatory:  dep.Mandatory,
					Type:       types.NormalizeDependencyType(dep.Type),
				})
			}
			pkg.Dependencies = types.PackageDependencies{Value: deps}
		}
		pkgs = append(pkgs, pkg)
	}
	return pkgs
}

// discoveredFromUpstream builds a discovered package from an injected
// upstream identity alone. It covers artifacts whose contents carry no
// readable manifest.
func discoveredFromUpstream(
	path string,
	ref types.VersionedPackageRef,
) types.DiscoveredPackage {
	platform := ref.Eco
	if platform == types.EcoUnspecified {
		platform = types.EcoForge
	}
	version := ref.Version
	if version == "" {
		version = types.VersionUnknown
	}
	pkgName := ref.Name
	if ref.Source == types.SourceMCDR {
		pkgName = types.BarePackageName(
			strings.ReplaceAll(string(ref.Name), "_", "-"),
		)
	}
	return types.DiscoveredPackage{
		Id: types.VersionedPackageRef{
			PackageRef: types.PackageRef{
				Name:   pkgName,
				Source: ref.Source,
			},
			Eco:     platform,
			Version: version,
		},
		Path: path,
	}
}
