package topology

import "github.com/mclucy/lucy/types"

type ConnectionDefinition struct {
	Source       ConnectionSource
	TargetNodeID types.RuntimeNodeID
	Kind         types.RuntimeEdgeVerb
}

func (d ConnectionDefinition) EdgeFrom(sourceNodeID types.RuntimeNodeID) types.RuntimeEdge {
	return types.RuntimeEdge{
		From: sourceNodeID,
		To:   d.TargetNodeID,
		Verb: d.Kind,
	}
}
