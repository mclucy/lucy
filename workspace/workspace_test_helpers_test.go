package workspace

import (
	"testing"

	"github.com/mclucy/lucy/types"
)

func makeDiscoveredPackage(
	t *testing.T,
	platform types.Ecosystem,
	name, version, path string,
) types.DiscoveredPackage {
	t.Helper()
	return types.DiscoveredPackage{
		Id: types.VersionedPackageRef{
			PackageRef: types.PackageRef{
				Eco:  platform,
				Name: types.BarePackageName(name),
			},
			Version: types.BareVersion(version),
		},
		Path: path,
	}
}

// makeNode builds a RuntimeNode for topology construction in tests.
func makeNode(
	id types.RuntimeNodeID,
	caps ...types.RuntimeCapability,
) types.RuntimeNode {
	return types.RuntimeNode{
		ID:           id,
		Capabilities: caps,
	}
}

// makeEdge builds a RuntimeEdge.
func makeEdge(
	from, to types.RuntimeNodeID,
	kind types.RuntimeEdgeVerb,
) types.RuntimeEdge {
	return types.RuntimeEdge{From: from, To: to, Verb: kind}
}

// makeTopology builds a RuntimeTopology with the given primary node, nodes, and edges.
func makeTopology(
	primary types.RuntimeNodeID,
	nodes []types.RuntimeNode,
	edges []types.RuntimeEdge,
) *types.ServerTopology {
	return &types.ServerTopology{
		PrimaryNode: primary,
		Nodes:       nodes,
		Edges:       edges,
	}
}
