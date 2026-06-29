package topology

import "github.com/mclucy/lucy/types"

type ConnectionSourceType string

const (
	ConnectionSourceNode       ConnectionSourceType = "node"
	ConnectionSourceCapability ConnectionSourceType = "capability"
)

type ConnectionSource struct {
	Type       ConnectionSourceType
	NodeID     types.RuntimeNodeID
	Capability types.RuntimeCapability
}

func SourceNode(id types.RuntimeNodeID) ConnectionSource {
	return ConnectionSource{
		Type:   ConnectionSourceNode,
		NodeID: id,
	}
}

func SourceCapability(capability types.RuntimeCapability) ConnectionSource {
	return ConnectionSource{
		Type:       ConnectionSourceCapability,
		Capability: capability,
	}
}
