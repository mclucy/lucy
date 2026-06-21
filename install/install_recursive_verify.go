package install

import (
	"fmt"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/internal/knownpkgs"
	"github.com/mclucy/lucy/types"
)

// VerifyDownloadedArtifacts analyzes locally-downloaded artifacts and replaces
// advisory dependency facts with authoritative detector output.
func VerifyDownloadedArtifacts(tx *RecursiveTransaction) error {
	if tx == nil {
		return fmt.Errorf("install: nil recursive transaction")
	}

	allPackages := make([]types.Package, 0, len(tx.DownloadedArtifacts))
	expectedPlatforms := downloadedArtifactPlatforms(tx)
	for _, path := range tx.DownloadedArtifacts {
		infos, err := artifact.Analyze(path)
		infos = selectArtifactInfosForPlatform(infos, expectedPlatforms[path])
		if err != nil || len(infos) == 0 {
			return fmt.Errorf(
				"install: artifact verification failed for %s: unreadable or corrupt",
				path,
			)
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
			Package:        pkg,
			ProvenancePath: []string{"verified"},
			Advisory:       false,
		}
	}

	tx.VerifiedGraph = verified
	tx.AdvanceTo(PhaseVerified)
	return nil
}

func downloadedArtifactPlatforms(tx *RecursiveTransaction) map[string]types.PlatformId {
	platforms := make(map[string]types.PlatformId, len(tx.DownloadedArtifacts))
	for _, node := range tx.CandidateGraph {
		if node.Package.Local == nil || node.Package.Local.Path == "" {
			continue
		}
		platforms[node.Package.Local.Path] = node.Package.Id.Platform
	}
	return platforms
}

func selectArtifactInfosForPlatform(
	infos []artifact.ArtifactInfo,
	platform types.PlatformId,
) []artifact.ArtifactInfo {
	// Cross-loader Forge/NeoForge jars can advertise both loader dependencies in
	// one descriptor; install verification keeps the identity matching the resolved
	// candidate so a single downloaded file is applied once.
	// Forge docs: https://docs.minecraftforge.net/en/1.21.x/gettingstarted/modfiles/
	// NeoForge docs: https://docs.neoforged.net/docs/1.20.4/gettingstarted/modfiles/
	if len(infos) == 0 || !platform.IsModding() {
		return infos
	}

	selected := make([]artifact.ArtifactInfo, 0, len(infos))
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

func artifactInfoToPackage(infos []artifact.ArtifactInfo) []types.Package {
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
			Supports:    info.Supports,
			Information: &info.Metadata,
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
						Embedded:   dep.Embedded,
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

func normalizeVerifiedPackage(pkg *types.Package) {
	sm := knownpkgs.Default()
	src := sourceForPlatform(pkg.Id.Platform)
	if src == types.SourceUnknown {
		return
	}

	if slug, ok := sm.GetLoose(src, string(pkg.Id.Name)); ok {
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
		if slug, ok := sm.GetLoose(depSrc, string(dep.Id.Name)); ok {
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
