package install

import (
	"context"
	"fmt"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/internal/artifacthash"
	"github.com/mclucy/lucy/internal/knownpkgs"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream/routing"
)

func verifyArtifacts(
	ctx context.Context,
	downloaded DownloadedClosure,
	journal Journal,
) (VerifiedClosure, error) {
	if err := ctx.Err(); err != nil {
		return VerifiedClosure{}, err
	}

	verifiedGraph, err := verifyDownloadedArtifacts(downloaded)
	if err != nil {
		return VerifiedClosure{}, err
	}

	diff, err := reconcileClosure(downloaded.Resolved, verifiedGraph, downloaded.Resolved.Ambient, journal)
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
) (map[string]CandidateNode, error) {
	allPackages := make([]types.Package, 0, len(downloaded.DownloadedArtifacts))
	expectedPackages := downloadedArtifactPackages(
		downloaded.Resolved.CandidateGraph,
	)
	for _, path := range downloaded.DownloadedArtifacts {
		infos, err := artifact.Analyze(path)
		expected, hasExpected := expectedPackages[path]
		if hasExpected {
			infos = selectArtifactInfosForPlatform(infos, expected.Id.Platform)
		}
		if err != nil {
			return nil, fmt.Errorf(
				"install: artifact verification failed for %s: unreadable or corrupt",
				path,
			)
		}
		if len(infos) == 0 {
			if !hasExpected || !hashMatchesResolvedPackage(path, expected) {
				return nil, fmt.Errorf(
					"install: artifact verification failed for %s: unreadable or corrupt",
					path,
				)
			}
			allPackages = append(allPackages, packageFromResolvedArtifact(path, expected))
			continue
		}
		if hasExpected {
			allPackages = append(
				allPackages,
				artifactInfoToResolvedPackages(infos, expected)...,
			)
			continue
		}
		allPackages = append(allPackages, artifactInfoToPackage(infos)...)
	}

	verified := make(map[string]CandidateNode, len(allPackages))
	for _, pkg := range allPackages {
		normalizeVerifiedPackage(&pkg)
		if pkg.Dependencies != nil {
			pkg.Dependencies.Authentic = true
		}

		verified[pkg.Id.StringBase()] = CandidateNode{
			Package:        resolvedPackageFromVerified(pkg),
			Path:           legacyPackagePath(pkg),
			Dependencies:   pkg.Dependencies,
			ProvenancePath: []string{"verified"},
			Advisory:       false,
		}
	}

	return verified, nil
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

func legacyPackagePath(pkg types.Package) string {
	if pkg.Local == nil {
		return ""
	}
	return pkg.Local.Path
}

func selectArtifactInfosForPlatform(
	infos []artifact.Info,
	platform types.PlatformId,
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
		if info.Ref.Platform == platform {
			selected = append(selected, info)
		}
	}
	if len(selected) == 0 {
		return infos
	}
	return selected
}

func artifactInfoToPackage(infos []artifact.Info) []types.Package {
	if len(infos) == 0 {
		return nil
	}
	pkgs := make([]types.Package, 0, len(infos))
	for _, info := range infos {
		pkg := types.Package{
			Id: types.VersionedPackageRef{
				PackageRef: types.PackageRef{
					Platform: info.Ref.Platform,
					Name:     info.Ref.Name,
				},
				Version: info.Version,
			},
			Local: &types.PackageInstallation{
				Path: info.FilePath,
			},
		}
		if len(info.Dependencies) > 0 {
			deps := make([]types.Dependency, 0, len(info.Dependencies))
			for _, dep := range info.Dependencies {
				deps = append(
					deps, types.Dependency{
						Id: types.VersionedPackageRef{
							PackageRef: types.PackageRef{
								Platform: dep.Ref.Platform,
								Name:     dep.Ref.Name,
							},
						},
						Constraint: dep.Constraint,
						Mandatory:  dep.Mandatory,
						Type:       types.NormalizeDependencyType(dep.Type),
					},
				)
			}
			pkg.Dependencies = &types.PackageDependencies{
				Value: deps,
			}
		}
		pkgs = append(pkgs, pkg)
	}
	return pkgs
}

func artifactInfoToResolvedPackages(
	infos []artifact.Info,
	resolved types.ResolvedPackage,
) []types.Package {
	packages := artifactInfoToPackage(infos)
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

func shouldPreserveResolvedIdentity(
	verified types.VersionedPackageRef,
	resolved types.FullPackageRef,
) bool {
	if resolved.Name == "" || resolved.Version == "" {
		return false
	}
	if verified.Platform != resolved.Platform {
		return false
	}
	return verified.Name != resolved.Name
}

func packageFromResolvedArtifact(
	path string,
	resolved types.ResolvedPackage,
) types.Package {
	return types.Package{
		Id: types.VersionedPackageRef{
			PackageRef: resolved.Id.PackageRef,
			Version:    resolved.Id.Version,
		},
		Local: &types.PackageInstallation{Path: path},
	}
}

func hashMatchesResolvedPackage(path string, resolved types.ResolvedPackage) bool {
	mapper, ok, err := routing.GetArtifactMapper(resolved.Id.Scope)
	if err != nil || !ok {
		return false
	}
	name, _, err := mapper.NameByHash(artifacthash.File{Path: path})
	if err != nil {
		return false
	}
	return name.RemoteName == string(resolved.Id.Name)
}

func normalizeVerifiedPackage(pkg *types.Package) {
	sess := knownpkgs.Default().Session()
	src := sourceForPlatform(pkg.Id.Platform)
	if src == types.SourceUnknown {
		return
	}

	if slug, ok := sess.Lookup(src, string(pkg.Id.Name)); ok {
		pkg.Id.Name = types.BarePackageName(slug)
	}

	if pkg.Dependencies == nil {
		return
	}
	for i, dep := range pkg.Dependencies.Value {
		depSrc := sourceForPlatform(dep.Id.Platform)
		if depSrc == types.SourceUnknown {
			continue
		}
		if slug, ok := sess.Lookup(depSrc, string(dep.Id.Name)); ok {
			pkg.Dependencies.Value[i].Id.Name = types.BarePackageName(slug)
		}
	}
}

func sourceForPlatform(p types.PlatformId) types.SourceId {
	switch p {
	case types.PlatformFabric, types.PlatformForge, types.PlatformNeoforge:
		return types.SourceModrinth
	case types.PlatformMCDR:
		return types.SourceMCDR
	default:
		return types.SourceUnknown
	}
}

func resolvedPackageFromVerified(pkg types.Package) types.ResolvedPackage {
	return types.ResolvedPackage{
		Id: types.FullPackageRef{
			PackageRef: pkg.Id.PackageRef,
			Version:    pkg.Id.Version,
			Scope:      types.SourceUnknown,
		},
	}
}
