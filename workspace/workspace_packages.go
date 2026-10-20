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
	mappers := []upstream.ArtifactMapProvider{modrinth.Provider}
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
// mcdrPluginDirs, when the MCDR wrapper exists. Deduplication and
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
			wg.Go(func() {
				analyzed, err := artifact.ReadInfo(jarPath)

				if err != nil || len(analyzed) == 0 {
					// Only a jar without a readable descriptor needs its upstream
					// identity, since anything else describes itself.
					if upstreamRef, hit := resolveUpstream(jarPath); hit {
						mu.Lock()
						idx.Add(discoveredFromUpstream(jarPath, upstreamRef))
						mu.Unlock()
					}
					return
				}
				pkgs := artifactInfoToDiscoveredPackage(analyzed)

				mu.Lock()
				idx.Merge(pkgs)
				mu.Unlock()
			})
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
			analyzed, err := artifact.ReadInfo(pluginFile)
			if err == nil && len(analyzed) > 0 {
				pkgs := artifactInfoToDiscoveredPackage(analyzed)
				idx.Merge(pkgs)
			}
		}
	}

	return idx.Packages()
}

func artifactInfoToDiscoveredPackage(infos []artifact.PackageInfo) []types.DiscoveredPackage {
	if len(infos) == 0 {
		return nil
	}
	pkgs := make([]types.DiscoveredPackage, 0, len(infos))
	for _, info := range infos {
		pkg := types.DiscoveredPackage{
			ID: types.VersionedPackageRef{
				PackageRef: info.Ref.PackageRef,
				Ecosystem:  info.Ref.Ecosystem,
				Version:    info.Ref.Version,
			},
			Path: info.FilePath,
		}
		if len(info.Dependencies) > 0 {
			deps := make([]types.Dependency, 0, len(info.Dependencies))
			for _, dep := range info.Dependencies {
				deps = append(deps, types.Dependency{
					ID: types.VersionedPackageRef{
						PackageRef: dep.Ref.PackageRef,
						Ecosystem:  dep.Ref.Ecosystem,
					},
					Constraint: dep.Constraint,
					Mandatory:  dep.Mandatory,
					Origin:     types.NormalizeDependencyOrigin(dep.Origin),
				})
			}
			pkg.Dependencies = types.DependencySet{Dependencies: deps}
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
	ecosystem := ref.Ecosystem
	if ecosystem == types.EcosystemUnspecified {
		ecosystem = types.EcosystemForge
	}
	version := ref.Version
	if version == "" {
		version = types.VersionUnknown
	}
	pkgName := ref.Name
	if ref.Provider == types.ProviderMCDR {
		pkgName = types.PackageName(
			strings.ReplaceAll(string(ref.Name), "_", "-"),
		)
	}
	return types.DiscoveredPackage{
		ID: types.VersionedPackageRef{
			PackageRef: types.PackageRef{
				Name:     pkgName,
				Provider: ref.Provider,
			},
			Ecosystem: ecosystem,
			Version:   version,
		},
		Path: path,
	}
}
