package workspace

import (
	"context"
	"strings"
	"sync"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/internal/knownpkgs"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/upstream/providers/curseforge"
	"github.com/mclucy/lucy/upstream/providers/modrinth"
)

// resolveUpstream resolves the following upstream of an artifact by hash,
// Modrinth first. A hit records the local-to-remote mapping in the session
// store, so later resolutions follow stable upstream identities across
// provider name differences.
func resolveUpstream(
	sess *knownpkgs.Session,
	path string,
	local *types.PackageRef,
) (types.FullPackageRef, bool) {
	mappers := []upstream.ArtifactMapSource{modrinth.Provider}
	if curseforge.Enabled() {
		mappers = append(mappers, curseforge.Provider)
	}

	for _, mapper := range mappers {
		ref, fileHash, ok, err := mapper.PackageByHash(
			artifact.File{Path: path},
		)
		if err != nil || !ok || ref.Name == "" {
			continue
		}
		if local != nil {
			sess.Record(
				mapper.Id(),
				local.Name.String(),
				fileHash,
				string(ref.Name),
				"hash",
			)
		}
		return ref, true
	}
	return types.FullPackageRef{}, false
}

// discoverPackages fills in the package index of one workspace probe. It
// analyzes every jar under searchPaths and every MCDR plugins under
// mcdrPluginDirs, when the MCDR environment exists. Results are
// deduplicated and sorted. Two discoveries of one package keep the entry
// that has a local path.
//
// Expensive. It opens every candidate artifact. Every artifact also goes
// through a hash query. A hit anchors the jar to a stable upstream
// identity and records the local-to-remote mapping.
func discoverPackages(
	sess *knownpkgs.Session,
	searchPaths []string,
	mcdrPluginDirs []string,
) []types.DiscoveredPackage {
	idx := NewPackageIndex()
	var mu sync.Mutex

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

				var local *types.PackageRef
				if err == nil && len(analyzed) == 1 {
					local = &analyzed[0].Ref
				}
				upstreamRef, hit := resolveUpstream(sess, path, local)

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
			analyzed, err := artifact.Analyze(
				pluginFile,
				artifact.WithSlugResolver(resolver),
			)

			var local *types.PackageRef
			if err == nil && len(analyzed) == 1 {
				local = &analyzed[0].Ref
			}
			resolveUpstream(sess, pluginFile, local)
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

// discoveredFromUpstream builds a discovered package from an injected
// upstream identity alone. It covers artifacts whose contents carry no
// readable manifest.
func discoveredFromUpstream(
	path string,
	ref types.FullPackageRef,
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
		Path: path,
	}
}

// knownPackagesSlugResolver returns a slug resolver that consults the
// knownpkgs session for a canonical name matching the detected
// platform/local name.
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
