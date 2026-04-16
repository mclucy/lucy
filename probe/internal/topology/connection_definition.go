package topology

import "github.com/mclucy/lucy/types"

type ConnectionDefinition struct {
	Source       ConnectionSource
	TargetNodeID types.RuntimeNodeID
	Kind         types.RuntimeEdgeKind
	Risk         types.RuntimeRiskLevel
}

func (d ConnectionDefinition) EdgeFrom(sourceNodeID types.RuntimeNodeID) types.RuntimeEdge {
	return types.RuntimeEdge{
		From: sourceNodeID,
		To:   d.TargetNodeID,
		Kind: d.Kind,
		Risk: d.Risk,
	}
}
