package install

import (
	"fmt"

	"github.com/mclucy/lucy/probe"
)

// VerifyDownloadedArtifacts analyzes locally-downloaded artifacts and replaces
// advisory dependency facts with authoritative detector output.
func VerifyDownloadedArtifacts(tx *RecursiveTransaction) error {
	if tx == nil {
		return fmt.Errorf("install: nil recursive transaction")
	}

	verified := make(map[string]CandidateNode)
	for _, path := range tx.DownloadedArtifacts {
		packages := probe.DetectPackages(path)
		if len(packages) == 0 {
			return fmt.Errorf("install: artifact verification failed for %s: unreadable or corrupt", path)
		}

		for _, pkg := range packages {
			if pkg.Dependencies != nil {
				pkg.Dependencies.Authentic = true
			}

			verified[pkg.Id.StringPlatformName()] = CandidateNode{
				Package:        pkg,
				ProvenancePath: []string{"verified"},
				Advisory:       false,
			}
		}
	}

	tx.VerifiedGraph = verified
	tx.AdvanceTo(PhaseVerified)
	return nil
}
