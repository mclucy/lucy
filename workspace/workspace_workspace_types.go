package workspace

import (
	"github.com/mclucy/lucy/types"
)

type Workspace struct {
	Root         string                    `json:"root"` // if found lucy on upper workspace
	SavePath     string                    `json:"save_path"`
	ModPath      []string                  `json:"mod_path"`
	Packages     []types.DiscoveredPackage `json:"packages"`
	Server       *ServerInstance           `json:"server,omitempty"`
	Topology     *types.ServerTopology     `json:"topology,omitempty"`
	Activity     *ServerActivity           `json:"activity,omitempty"`
	Environments types.EnvironmentInfo     `json:"environments"`
}

func (w Workspace) RuntimeIdentityPackage(node *types.TopologyNode) *types.VersionedPackageRef {
	if w.Server == nil || node == nil {
		return nil
	}

	return runtimeIdentityPackage(w.Topology, node)
}

func (w Workspace) PrimaryRuntimeIdentity() *types.VersionedPackageRef {
	if w.Server == nil {
		return nil
	}

	return w.Server.PrimaryCore
}

func (w Workspace) DerivedLoaderVersion() string {
	if w.Server == nil {
		return "unknown"
	}

	return w.Server.DerivedLoaderVersion()
}

func (w Workspace) DerivedModLoader() types.Ecosystem {
	if w.Server == nil {
		return types.EcoUnspecified
	}
	return w.Server.DerivedModLoader()
}

func (w Workspace) DerivedServerCore() string {
	if w.Server == nil {
		return ""
	}
	return w.Server.DerivedServerCore()
}

func runtimeIdentityPackage(
	topology *types.ServerTopology,
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

type ServerActivity struct {
	Active bool `json:"active"`
	Pid    int  `json:"pid"`
}
