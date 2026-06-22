package workspace

import (
	"github.com/mclucy/lucy/types"
)

type Workspace struct {
	Root         string                 `json:"root"` // if found lucy on upperworkspace
	SavePath     string                 `json:"save_path"`
	ModPath      []string               `json:"mod_path"`
	Packages     []types.Package        `json:"packages"`
	Runtime      *ServerRuntime         `json:"runtime,omitempty"`
	Topology     *types.RuntimeTopology `json:"topology,omitempty"`
	Activity     *ServerActivity        `json:"activity,omitempty"`
	Environments types.EnvironmentInfo  `json:"environments"`
	McdrRoot     string                 `json:"mcdr_root"`
	LucyRoot     string                 `json:"lucy_root"`
}

func (w Workspace) RuntimeIdentityPackage(node *types.TopologyNode) *types.VersionedPackageRef {
	if w.Runtime == nil || node == nil {
		return nil
	}

	return runtimeIdentityPackage(w.Runtime.RuntimeIdentities, node)
}

func (w Workspace) PrimaryRuntimeIdentity() *types.VersionedPackageRef {
	if w.Runtime == nil {
		return nil
	}

	return primaryRuntimeIdentity(w.Topology, w.Runtime.RuntimeIdentities)
}

func (w Workspace) DerivedLoaderVersion() string {
	if w.Runtime == nil {
		return derivedLoaderVersion(nil, nil)
	}

	return derivedLoaderVersion(w.Topology, w.Runtime.RuntimeIdentities)
}

func (w Workspace) DerivedModLoader() types.PlatformId {
	return derivedModLoader(w.Topology)
}

func (w Workspace) DerivedServerCore() string {
	return derivedServerCore(w.Topology)
}

func runtimeIdentityPackage(
	identities []types.VersionedPackageRef,
	node *types.TopologyNode,
) *types.VersionedPackageRef {
	if node == nil {
		return nil
	}

	for i := range identities {
		pkg := &identities[i]
		if string(pkg.Name) == string(node.ID) {
			return pkg
		}
	}

	return nil
}

func primaryRuntimeIdentity(
	topology *types.RuntimeTopology,
	identities []types.VersionedPackageRef,
) *types.VersionedPackageRef {
	if topology == nil {
		return nil
	}

	primaryNode, ok := topology.PrimaryNodeData()
	if !ok {
		return nil
	}

	return runtimeIdentityPackage(identities, &primaryNode)
}

func derivedLoaderVersion(
	topology *types.RuntimeTopology,
	identities []types.VersionedPackageRef,
) string {
	primaryIdentity := primaryRuntimeIdentity(topology, identities)
	if primaryIdentity == nil {
		return "unknown"
	}

	return primaryIdentity.Version.String()
}

func derivedModLoader(topology *types.RuntimeTopology) types.PlatformId {
	if topology == nil {
		return types.PlatformNone
	}

	primary, ok := topology.PrimaryNodeData()
	if !ok {
		return types.PlatformNone
	}

	return types.DeclaredModdingPlatformForNode(primary.ID)
}

func derivedServerCore(topology *types.RuntimeTopology) string {
	if topology == nil {
		return ""
	}

	primary, ok := topology.PrimaryNodeData()
	if !ok {
		return ""
	}

	return string(primary.ID)
}

type ServerActivity struct {
	Active bool `json:"active"`
	Pid    int  `json:"pid"`
}
