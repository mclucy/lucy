package detector

import "github.com/mclucy/lucy/types"

// ExecutableEvidence is the detector output consumed by workspace assembly.
// PrimaryRuntime identifies the selected bootable artifact. A nil primary does
// not establish an executable runtime.
type ExecutableEvidence struct {
	PrimaryRuntime    *types.VersionedPackageRef
	PrimaryPath       string
	RuntimeComponents []types.VersionedPackageRef

	// DetectorName identifies the detector that produced this candidate.
	DetectorName string
}

// ExecutableCandidates groups all detector candidates for one executable so the
// aggregator can resolve ambiguity before building the ServerInstance.
type ExecutableCandidates struct {
	Candidates []*ExecutableEvidence
}

func (c *ExecutableCandidates) IsEmpty() bool {
	return c == nil || len(c.Candidates) == 0
}

func (c *ExecutableCandidates) IsAmbiguous() bool {
	return c != nil && len(c.disambiguated()) > 1
}

func (c *ExecutableCandidates) Single() *ExecutableEvidence {
	filtered := c.disambiguated()
	if len(filtered) != 1 {
		return nil
	}
	return filtered[0]
}

// disambiguated drops generic vanilla fallback evidence when a more specific
// executable detector also matched the same artifact.
func (c *ExecutableCandidates) disambiguated() []*ExecutableEvidence {
	if c == nil || len(c.Candidates) <= 1 {
		if c == nil {
			return nil
		}
		return c.Candidates
	}

	hasSpecific := false
	for _, cand := range c.Candidates {
		if !isVanillaEvidence(cand) {
			hasSpecific = true
			break
		}
	}
	if !hasSpecific {
		return c.Candidates
	}

	disambiguated := make([]*ExecutableEvidence, 0, len(c.Candidates))
	for _, cand := range c.Candidates {
		if !isVanillaEvidence(cand) {
			disambiguated = append(disambiguated, cand)
		}
	}
	return disambiguated
}

func isVanillaEvidence(cand *ExecutableEvidence) bool {
	return cand != nil &&
		cand.PrimaryRuntime != nil &&
		cand.PrimaryRuntime.Eco == types.EcoMinecraft &&
		cand.PrimaryRuntime.Name == "minecraft"
}
