package probe

import (
	"testing"

	"github.com/mclucy/lucy/types"
)

// makePackage builds a minimal types.Package for use in tests.
// localPath may be "" to simulate a remote-only entry.
func makePackage(t *testing.T, platform types.Platform, name, version, localPath string) types.Package {
	t.Helper()
	pkg := types.Package{
		Id: types.PackageId{
			Platform: platform,
			Name:     types.ProjectName(name),
			Version:  types.RawVersion(version),
		},
	}
	if localPath != "" {
		pkg.Local = &types.PackageInstallation{Path: localPath}
	}
	return pkg
}

// makeNode builds a RuntimeNode for topology construction in tests.
func makeNode(id types.RuntimeNodeID, caps ...types.RuntimeCapability) types.RuntimeNode {
	return types.RuntimeNode{
		ID:           id,
		Capabilities: caps,
	}
}

// makeEdge builds a RuntimeEdge.
func makeEdge(from, to types.RuntimeNodeID, kind types.RuntimeEdgeKind, risk types.RuntimeRiskLevel) types.RuntimeEdge {
	return types.RuntimeEdge{From: from, To: to, Kind: kind, Risk: risk}
}

// makeTopology builds a RuntimeTopology with the given primary node, nodes, and edges.
func makeTopology(primary types.RuntimeNodeID, nodes []types.RuntimeNode, edges []types.RuntimeEdge) *types.RuntimeTopology {
	return &types.RuntimeTopology{
		PrimaryNode: primary,
		Nodes:       nodes,
		Edges:       edges,
	}
}
