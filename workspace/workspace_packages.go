package workspace

import (
	"context"
	"strings"
	"sync"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/internal/artifacthash"
	"github.com/mclucy/lucy/internal/knownpkgs"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/upstream/providers/curseforge"
	"github.com/mclucy/lucy/upstream/providers/modrinth"
)

// discoverPackages fills in the package index of one workspace probe. It
// analyzes every jar under searchPaths and every MCDR plugins under
// mcdrPluginDirs, when the MCDR environment exists. Results are
// deduplicated and sorted. Two discoveries of one package keep the entry
// that has a local path.
//
// Expensive. It opens every candidate artifact. Jars without readable
// metadata fall back to hash lookups at providers. These lookups can touch
// the network.
func discoverPackages(
	searchPaths []string,
	mcdrPluginDirs []string,
) []types.DiscoveredPackage {
	idx := NewPackageIndex()
	var mu sync.Mutex

	sess := knownpkgs.Default().Session()
	resolver := knownPackagesSlugResolver(sess)

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

				analyzed, err := artifact.Analyze(
					path,
					artifact.WithSlugResolver(resolver),
				)
				if err != nil || len(analyzed) == 0 {
					pkg, ok := packageByArtifactHash(path)
					if !ok {
						return
					}
					mu.Lock()
					idx.Add(pkg)
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
			analyzed, err := artifact.Analyze(
				pluginFile,
				artifact.WithSlugResolver(resolver),
			)
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
				PackageRef: types.PackageRef{
					Eco:  info.Ref.Eco,
					Name: info.Ref.Name,
				},
				Version: info.Version,
			},
			Path: info.FilePath,
		}
		if len(info.Dependencies) > 0 {
			deps := make([]types.Dependency, 0, len(info.Dependencies))
			for _, dep := range info.Dependencies {
				deps = append(
					deps, types.Dependency{
						Id: types.VersionedPackageRef{
							PackageRef: types.PackageRef{
								Eco:  dep.Ref.Eco,
								Name: dep.Ref.Name,
							},
						},
						Constraint: dep.Constraint,
						Mandatory:  dep.Mandatory,
						Type:       types.NormalizeDependencyType(dep.Type),
					},
				)
			}
			pkg.Dependencies = types.PackageDependencies{Value: deps}
		}
		pkgs = append(pkgs, pkg)
	}
	return pkgs
}

// packageByArtifactHash names a metadata-less artifact by asking hash-aware
// upstream providers. It handles jars whose contents carry no readable
// manifest.
func packageByArtifactHash(filePath string) (types.DiscoveredPackage, bool) {
	providers := []upstream.ArtifactMapSource{modrinth.Provider}
	if curseforge.Enabled() {
		providers = append(providers, curseforge.Provider)
	}
	for _, mapper := range providers {
		ref, _, ok, err := mapper.PackageByHash(artifacthash.File{Path: filePath})
		if err != nil || !ok || ref.Name == "" {
			continue
		}
		platform := ref.Eco
		if platform == types.EcoUnspecified {
			platform = types.EcoForge
		}
		version := ref.Version
		if version == "" {
			version = types.VersionUnknown
		}
		pkgName := ref.Name
		if ref.Scope == types.SourceMCDR {
			pkgName = types.BarePackageName(
				strings.ReplaceAll(string(ref.Name), "_", "-"),
			)
		}
		return types.DiscoveredPackage{
			Id: types.VersionedPackageRef{
				PackageRef: types.PackageRef{
					Eco:  platform,
					Name: pkgName,
				},
				Version: version,
			},
			Path: filePath,
		}, true
	}
	return types.DiscoveredPackage{}, false
}

// knownPackagesSlugResolver returns a slug resolver that consults the knownpkgs
// session for a canonical name matching the detected platform/local name.
//
// On hit, the mapping is promoted into the session cache via Record so that
// subsequent resolutions in the same invocation see the freshly discovered
// mapping without re-querying.
func knownPackagesSlugResolver(session *knownpkgs.Session) artifact.SlugResolver {
	return func(
		ctx context.Context,
		platform types.Ecosystem,
		name types.BarePackageName,
	) (types.BarePackageName, error) {
		canonical, src, ok := session.LookupAny(string(name))
		if !ok || canonical == string(name) {
			return name, nil
		}
		// Resolver runs on the local name only, not on file contents — the
		// persisted store already holds this mapping (LookupAny hit it).
		session.Record(src, string(name), "", canonical, "hash")
		return types.BarePackageName(canonical), nil
	}
}
