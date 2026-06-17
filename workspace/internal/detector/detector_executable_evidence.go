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
	BridgeHints       []string
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
	return c != nil && len(c.Candidates) > 1
}

func (c *ExecutableCandidates) Single() *ExecutableEvidence {
	if c == nil || len(c.Candidates) != 1 {
		return nil
	}
	return c.Candidates[0]
}
