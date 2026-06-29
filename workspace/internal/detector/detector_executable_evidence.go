package detector

import "github.com/mclucy/lucy/types"

// ExecutableDetectorProvenance records which detector produced an executable
// evidence candidate. This remains internal to probe/detector flow even though
// the type name is exported within the package surface for current refactor
// compatibility.
type ExecutableDetectorProvenance struct {
	DetectorName string
}

// ExecutableTopologySeed captures detector-produced topology facts before final
// RuntimeInfo assembly and downstream topology enrichment choose the canonical
// runtime topology.
type ExecutableTopologySeed struct {
	PrimaryNode types.RuntimeNodeID
	Nodes       []types.RuntimeNode
	Edges       []types.RuntimeEdge
}

// ExecutableEvidence is the internal detector output contract. It separates raw
// detection evidence from final public RuntimeInfo assembly while still keeping
// the current detector package compatible during the refactor.
type ExecutableEvidence struct {
	PrimaryEntrance   string
	GameVersion       types.BareVersion
	Topology          *types.RuntimeTopology
	TopologySeed      *ExecutableTopologySeed
	RuntimeIdentities []types.VersionedPackageRef
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

// resolved returns candidates after dropping generic fallback evidence when
// more specific detectors also fired on the same jar. This lets the vanilla
// catch-all (RuntimeNodeMinecraft) yield to Paper, Forge, Fabric, etc. without
// producing false ambiguity.
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
	if cand == nil {
		return false
	}
	if cand.Topology != nil && cand.Topology.PrimaryNode == types.RuntimeNodeMinecraft {
		return true
	}
	if cand.TopologySeed != nil && cand.TopologySeed.PrimaryNode == types.RuntimeNodeMinecraft {
		return true
	}
	return false
}
