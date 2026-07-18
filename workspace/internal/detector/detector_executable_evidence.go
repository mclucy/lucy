package detector

import "github.com/mclucy/lucy/types"

// ExecutableDetectorProvenance identifies the detector that produced an
// executable candidate.
type ExecutableDetectorProvenance struct {
	DetectorName string
}

// ExecutableEvidence is the detector output consumed by workspace assembly.
// PrimaryRuntime identifies the selected bootable artifact. A nil primary does
// not establish an executable runtime.
type ExecutableEvidence struct {
	PrimaryRuntime    *types.VersionedPackageRef
	PrimaryPath       string
	RuntimeComponents []types.VersionedPackageRef
	Provenance        ExecutableDetectorProvenance
}

// ExecutableCandidates groups all detector candidates for one executable so the
// aggregator can resolve ambiguity before materializing RuntimeInfo.
type ExecutableCandidates struct {
	Candidates []*ExecutableEvidence
}

func (c *ExecutableCandidates) IsEmpty() bool {
	return c == nil || len(c.Candidates) == 0
}

func (c *ExecutableCandidates) IsAmbiguous() bool {
	return c != nil && len(c.resolved()) > 1
}

func (c *ExecutableCandidates) Single() *ExecutableEvidence {
	resolved := c.resolved()
	if len(resolved) != 1 {
		return nil
	}
	return resolved[0]
}

// resolved drops generic vanilla fallback evidence when a more specific
// executable detector also matched the same artifact.
func (c *ExecutableCandidates) resolved() []*ExecutableEvidence {
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

	resolved := make([]*ExecutableEvidence, 0, len(c.Candidates))
	for _, cand := range c.Candidates {
		if !isVanillaEvidence(cand) {
			resolved = append(resolved, cand)
		}
	}
	return resolved
}

func isVanillaEvidence(cand *ExecutableEvidence) bool {
	return cand != nil &&
		cand.PrimaryRuntime != nil &&
		cand.PrimaryRuntime.Eco == types.EcoMinecraft &&
		cand.PrimaryRuntime.Name == "minecraft"
}
