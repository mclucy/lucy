package install

import (
	"fmt"

	"github.com/mclucy/lucy/probe"
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
		packages := probe.DetectPackages(path)
		if len(packages) == 0 {
			return fmt.Errorf("install: artifact verification failed for %s: unreadable or corrupt", path)
		}
		allPackages = append(allPackages, packages...)
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

func normalizeVerifiedPackage(pkg *types.Package) {
	sm := slugmap.Default()
	src := sourceForPlatform(pkg.Id.Platform)
	if src == types.SourceUnknown {
		return
	}

	if slug, ok := sm.GetLoose(src, string(pkg.Id.Name)); ok {
		pkg.Id.Name = types.ProjectName(slug)
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
			pkg.Dependencies.Value[i].Id.Name = types.ProjectName(slug)
		}
	}
}

func sourceForPlatform(p types.Platform) types.Source {
	switch p {
	case types.PlatformFabric, types.PlatformForge, types.PlatformNeoforge:
		return types.SourceModrinth
	case types.PlatformMCDR:
		return types.SourceMCDR
	default:
		return types.SourceUnknown
	}
}
