package install

import (
	"context"
	"fmt"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/internal/artifacthash"
	"github.com/mclucy/lucy/internal/knownpkgs"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream/routing"
	"github.com/mclucy/lucy/workspace"
)

func verifyArtifacts(
	ctx context.Context,
	downloaded DownloadedClosure,
	journal Journal,
	runtime *workspace.ServerInstance,
) (VerifiedClosure, error) {
	if err := ctx.Err(); err != nil {
		return VerifiedClosure{}, err
	}

	verifiedGraph, err := verifyDownloadedArtifacts(downloaded, runtime)
	if err != nil {
		return VerifiedClosure{}, err
	}

	diff, err := reconcileClosure(
		downloaded.Resolved,
		verifiedGraph,
		downloaded.Resolved.Ambient,
		journal,
	)
	if err != nil {
		return VerifiedClosure{}, err
	}

	return VerifiedClosure{
		Downloaded:    downloaded,
		VerifiedGraph: verifiedGraph,
		ReconcileDiff: diff,
	}, nil
}

func verifyDownloadedArtifacts(
	downloaded DownloadedClosure,
	runtime *workspace.ServerInstance,
) (map[string]CandidateNode, error) {
	allPackages := make(
		[]types.DiscoveredPackage,
		0,
		len(downloaded.DownloadedArtifacts),
	)
	expectedPackages := downloadedArtifactPackages(
		downloaded.Resolved.CandidateGraph,
	)
	for _, path := range downloaded.DownloadedArtifacts {
		infos, err := artifact.Analyze(path)
		expected, hasExpected := expectedPackages[path]
		if hasExpected {
			infos = selectArtifactInfosForEcosystem(infos, expected.Id.Eco)
		}
		if err != nil {
			return nil, fmt.Errorf(
				"install: artifact verification failed for %s: unreadable or corrupt",
				path,
			)
		}
		if err := ensureFoliaArtifactCompatibility(
			path,
			infos,
			expected,
			hasExpected,
			runtime,
		); err != nil {
			return nil, err
		}
		if len(infos) == 0 {
			if !hasExpected || !hashMatchesResolvedPackage(path, expected) {
				return nil, fmt.Errorf(
					"install: artifact verification failed for %s: unreadable or corrupt",
					path,
				)
			}
			allPackages = append(
				allPackages,
				discoveredFromResolvedArtifact(path, expected),
			)
			continue
		}
		if hasExpected {
			allPackages = append(
				allPackages,
				artifactInfoToResolvedDiscoveredPackages(infos, expected)...,
			)
			continue
		}
		allPackages = append(
			allPackages,
			artifactInfoToDiscoveredPackages(infos)...,
		)
	}

	verified := make(map[string]CandidateNode, len(allPackages))
	for _, pkg := range allPackages {
		normalizeVerifiedDiscoveredPackage(&pkg)
		var deps *types.PackageDependencies
		if len(pkg.Dependencies.Value) > 0 {
			d := pkg.Dependencies
			d.Authentic = true
			deps = &d
		}

		key := pkg.Id.StringBase()
		verified[key] = CandidateNode{
			Package:        resolvedPackageFromVerifiedDiscovered(pkg),
			Path:           pkg.Path,
			Dependencies:   deps,
			ProvenancePath: []string{"verified"},
			Advisory:       false,
		}
	}

	return verified, nil
}

func ensureFoliaArtifactCompatibility(
	path string,
	infos []artifact.Info,
	expected types.ResolvedPackage,
	hasExpected bool,
	runtime *workspace.ServerInstance,
) error {
	if !isFoliaRuntime(runtime) ||
		!hasExpected ||
		!isBukkitFamilyCandidate(expected.Id.Eco) {
		return nil
	}

	for _, info := range infos {
		if !isBukkitFamilyCandidate(info.Ref.Eco) {
			continue
		}
		if !info.Compatibility.FoliaSupported {
			return fmt.Errorf(
				"install: artifact verification failed for %s: bukkit plugin does not declare folia-supported: true",
				path,
			)
		}
	}
	return nil
}

func isBukkitFamilyCandidate(ecosystem types.Ecosystem) bool {
	return ecosystem == types.EcoBukkit || ecosystem == types.EcoPaper
}

func isFoliaRuntime(runtime *workspace.ServerInstance) bool {
	return runtime != nil &&
		runtime.PrimaryRuntime != nil &&
		runtime.PrimaryRuntime.Name == "folia"
}

func downloadedArtifactPackages(
	candidateGraph map[string]CandidateNode,
) map[string]types.ResolvedPackage {
	packages := make(map[string]types.ResolvedPackage, len(candidateGraph))
	for _, node := range candidateGraph {
		if node.Path == "" {
			continue
		}
		packages[node.Path] = node.Package
	}
	return packages
}

func selectArtifactInfosForEcosystem(
	infos []artifact.Info,
	platform types.Ecosystem,
) []artifact.Info {
	// Cross-loader Forge/NeoForge jars can advertise both loader dependencies in
	// one descriptor; install verification keeps the identity matching the resolved
	// candidate so a single downloaded file is applied once.
	// Forge docs: https://docs.minecraftforge.net/en/1.21.x/gettingstarted/modfiles/
	// NeoForge docs: https://docs.neoforged.net/docs/1.20.4/gettingstarted/modfiles/
	if len(infos) == 0 || !platform.IsModding() {
		return infos
	}

	selected := make([]artifact.Info, 0, len(infos))
	for _, info := range infos {
		if info.Ref.Eco == platform {
			selected = append(selected, info)
		}
	}
	if len(selected) == 0 {
		return infos
	}
	return selected
}

func artifactInfoToDiscoveredPackages(infos []artifact.Info) []types.DiscoveredPackage {
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
			pkg.Dependencies = packageDependenciesFromArtifact(info.Dependencies)
		}
		pkgs = append(pkgs, pkg)
	}
	return pkgs
}

func artifactInfoToResolvedDiscoveredPackages(
	infos []artifact.Info,
	resolved types.ResolvedPackage,
) []types.DiscoveredPackage {
	packages := artifactInfoToDiscoveredPackages(infos)
	for i := range packages {
		if shouldPreserveResolvedIdentity(packages[i].Id, resolved.Id) {
			packages[i].Id = types.VersionedPackageRef{
				PackageRef: resolved.Id.PackageRef,
				Version:    resolved.Id.Version,
			}
		}
	}
	return packages
}

func packageDependenciesFromArtifact(deps []artifact.Dependency) types.PackageDependencies {
	out := make([]types.Dependency, 0, len(deps))
	for _, dep := range deps {
		out = append(
			out, types.Dependency{
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
	return types.PackageDependencies{Value: out}
}

func shouldPreserveResolvedIdentity(
	verified types.VersionedPackageRef,
	resolved types.FullPackageRef,
) bool {
	if resolved.Name == "" || resolved.Version == "" {
		return false
	}
	if verified.Eco != resolved.Eco {
		return false
	}
	return verified.Name != resolved.Name
}

func discoveredFromResolvedArtifact(
	path string,
	resolved types.ResolvedPackage,
) types.DiscoveredPackage {
	return types.DiscoveredPackage{
		Id: types.VersionedPackageRef{
			PackageRef: resolved.Id.PackageRef,
			Version:    resolved.Id.Version,
		},
		Path: path,
	}
}

func hashMatchesResolvedPackage(
	path string,
	resolved types.ResolvedPackage,
) bool {
	mapper, ok, err := routing.GetArtifactMapper(resolved.Id.Scope)
	if err != nil || !ok {
		return false
	}
	ref, _, ok, err := mapper.PackageByHash(artifacthash.File{Path: path})
	if err != nil || !ok {
		return false
	}
	return ref.Name == resolved.Id.Name
}

func normalizeVerifiedDiscoveredPackage(pkg *types.DiscoveredPackage) {
	sess := knownpkgs.Default().Session()
	src := sourceForEcosystem(pkg.Id.Eco)
	if src == types.SourceUnknown {
		return
	}

	if slug, ok := sess.Lookup(src, string(pkg.Id.Name)); ok {
		pkg.Id.Name = types.BarePackageName(slug)
	}

	for i, dep := range pkg.Dependencies.Value {
		depSrc := sourceForEcosystem(dep.Id.Eco)
		if depSrc == types.SourceUnknown {
			continue
		}
		if slug, ok := sess.Lookup(depSrc, string(dep.Id.Name)); ok {
			pkg.Dependencies.Value[i].Id.Name = types.BarePackageName(slug)
		}
	}
}

func sourceForEcosystem(p types.Ecosystem) types.SourceId {
	switch p {
	case types.EcoFabric, types.EcoForge, types.EcoNeoforge:
		return types.SourceModrinth
	case types.EcoMcdr:
		return types.SourceMCDR
	default:
		return types.SourceUnknown
	}
}

func resolvedPackageFromVerifiedDiscovered(pkg types.DiscoveredPackage) types.ResolvedPackage {
	return types.ResolvedPackage{
		Id: types.FullPackageRef{
			PackageRef: pkg.Id.PackageRef,
			Version:    pkg.Id.Version,
			Scope:      types.SourceUnknown,
		},
	}
}
