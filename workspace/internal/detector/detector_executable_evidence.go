package detector

import "github.com/mclucy/lucy/types"

// RuntimeEvidence is the detector output consumed by workspace assembly.
// PrimaryRuntime identifies the selected bootable artifact. A nil primary does
// not establish an executable runtime.
type RuntimeEvidence struct {
	PrimaryRuntime    *types.VersionedPackageRef
	PrimaryPath       string
	RuntimeComponents []types.VersionedPackageRef

	// ConsumedFiles are artifacts absorbed by this runtime, such as the
	// vanilla jar a Fabric launch shim boots.
	ConsumedFiles []*DetectionFile

	DetectorName string
}

// IsVanilla reports evidence claiming a plain Minecraft runtime.
func (e *RuntimeEvidence) IsVanilla() bool {
	return e != nil && e.PrimaryRuntime != nil &&
		e.PrimaryRuntime.Ecosystem == types.EcosystemMinecraft &&
		e.PrimaryRuntime.Name == "minecraft"
}

// RuntimeClaims groups all detector candidates for one executable so the
// aggregator can resolve ambiguity before building the ServerRuntime.
type RuntimeClaims struct {
	Candidates []*RuntimeEvidence
}

func (c *RuntimeClaims) IsEmpty() bool {
	return c == nil || len(c.Candidates) == 0
}

func (c *RuntimeClaims) IsAmbiguous() bool {
	return c != nil && len(c.disambiguated()) > 1
}

func (c *RuntimeClaims) Single() *RuntimeEvidence {
	filtered := c.disambiguated()
	if len(filtered) != 1 {
		return nil
	}
	return filtered[0]
}

// disambiguated drops generic vanilla fallback evidence when a more specific
// executable detector also matched the same artifact.
func (c *RuntimeClaims) disambiguated() []*RuntimeEvidence {
	if c == nil || len(c.Candidates) <= 1 {
		if c == nil {
			return nil
		}
		return c.Candidates
	}

	hasSpecific := false
	for _, cand := range c.Candidates {
		if !cand.IsVanilla() {
			hasSpecific = true
			break
		}
	}
	if !hasSpecific {
		return c.Candidates
	}

	disambiguated := make([]*RuntimeEvidence, 0, len(c.Candidates))
	for _, cand := range c.Candidates {
		if !cand.IsVanilla() {
			disambiguated = append(disambiguated, cand)
		}
	}
	return disambiguated
}
