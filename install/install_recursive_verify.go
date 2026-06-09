package install

import (
	"fmt"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/slugmap"
	"github.com/mclucy/lucy/types"
)

// VerifyDownloadedArtifacts analyzes locally-downloaded artifacts and replaces
// advisory dependency facts with authoritative detector output.
func VerifyDownloadedArtifacts(tx *RecursiveTransaction) error {
	if tx == nil {
		return fmt.Errorf("install: nil recursive transaction")
	}

	allPackages := make([]types.Package, 0, len(tx.DownloadedArtifacts))
	for _, path := range tx.DownloadedArtifacts {
		infos, err := artifact.Analyze(path)
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

		verified[pkg.Id.StringPlatformName()] = CandidateNode{
			Package:        pkg,
			ProvenancePath: []string{"verified"},
			Advisory:       false,
		}
	}

	tx.VerifiedGraph = verified
	tx.AdvanceTo(PhaseVerified)
	return nil
}

func artifactInfoToPackage(infos []artifact.ArtifactInfo) []types.Package {
	if len(infos) == 0 {
		return nil
	}
	pkgs := make([]types.Package, 0, len(infos))
	for _, info := range infos {
		pkg := types.Package{
			Id: types.VersionedPackageRef{
				Platform: info.Ref.Platform,
				Name:     info.Ref.Name,
				Version:  info.Version,
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
							Platform: dep.Ref.Platform,
							Name:     dep.Ref.Name,
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
	sm := slugmap.Default()
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
