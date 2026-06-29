package workspace

import (
	"github.com/mclucy/lucy/types"
)

type Workspace struct {
	Root         string                    `json:"root"` // if found lucy on upperworkspace
	SavePath     string                    `json:"save_path"`
	ModPath      []string                  `json:"mod_path"`
	Packages     []types.DiscoveredPackage `json:"packages"`
	Runtime      *ServerRuntime            `json:"runtime,omitempty"`
	Topology     *types.RuntimeTopology    `json:"topology,omitempty"`
	Activity     *ServerActivity           `json:"activity,omitempty"`
	Environments types.EnvironmentInfo     `json:"environments"`
	McdrRoot     string                    `json:"mcdr_root"`
	LucyRoot     string                    `json:"lucy_root"`
}

func (w Workspace) RuntimeIdentityPackage(node *types.TopologyNode) *types.VersionedPackageRef {
	if w.Runtime == nil || node == nil {
		return nil
	}

	return runtimeIdentityPackage(w.Topology, node)
}

func (w Workspace) PrimaryRuntimeIdentity() *types.VersionedPackageRef {
	if w.Runtime == nil {
		return nil
	}

	return primaryRuntimeIdentity(w.Topology)
}

func (w Workspace) DerivedLoaderVersion() string {
	if w.Runtime == nil {
		return derivedLoaderVersion(nil)
	}

	return derivedLoaderVersion(w.Topology)
}

func (w Workspace) DerivedModLoader() types.Ecosystem {
	return derivedModLoader(w.Topology)
}

func (w Workspace) DerivedServerCore() string {
	return derivedServerCore(w.Topology)
}

func runtimeIdentityPackage(
	topology *types.RuntimeTopology,
	node *types.TopologyNode,
) *types.VersionedPackageRef {
	if topology == nil || node == nil {
		return nil
	}

	identities := topology.NodeIdentities(node.ID)
	for i := range identities {
		pkg := &identities[i]
		if string(pkg.Name) == string(node.ID) {
			return pkg
		}
	}

	return nil
}

func primaryRuntimeIdentity(topology *types.RuntimeTopology) *types.VersionedPackageRef {
	if topology == nil {
		return nil
	}

	identities := topology.NodeIdentities(topology.PrimaryNode)
	for i := range identities {
		pkg := &identities[i]
		if string(pkg.Name) == string(topology.PrimaryNode) {
			return pkg
		}
	}

	return nil
}

func derivedLoaderVersion(topology *types.RuntimeTopology) string {
	primaryIdentity := primaryRuntimeIdentity(topology)
	if primaryIdentity == nil {
		return "unknown"
	}

	return primaryIdentity.Version.String()
}

func derivedModLoader(topology *types.RuntimeTopology) types.Ecosystem {
	if topology == nil {
		return types.EcoBare
	}

	primary, ok := topology.PrimaryNodeData()
	if !ok {
		return types.EcoBare
	}

	return types.DeclaredModdingEcosystemForNode(primary.ID)
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
