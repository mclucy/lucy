package workspace

import (
	"github.com/mclucy/lucy/types"
)

// Workspace components that do not exist, use an empty string. Note Runtime
// must exist, otherwise the program will exit; therefore, it is not a pointer.
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

	for i := range w.Runtime.RuntimeIdentities {
		pkg := &w.Runtime.RuntimeIdentities[i]
		if string(pkg.Name) == string(node.ID) {
			return pkg
		}
	}

	return nil
}

func (w Workspace) PrimaryRuntimeIdentity() *types.VersionedPackageRef {
	if w.Topology == nil {
		return nil
	}

	primaryNode, ok := w.Topology.PrimaryNodeData()
	if !ok {
		return nil
	}

	return w.RuntimeIdentityPackage(&primaryNode)
}

func (w Workspace) DerivedLoaderVersion() string {
	primaryIdentity := w.PrimaryRuntimeIdentity()
	if primaryIdentity == nil {
		return "unknown"
	}

	return primaryIdentity.Version.String()
}

func (w Workspace) DerivedModLoader() types.PlatformId {
	if w.Topology == nil {
		return types.PlatformNone
	}

	primary, ok := w.Topology.PrimaryNodeData()
	if !ok {
		return types.PlatformNone
	}

	return types.DeclaredModdingPlatformForNode(primary.ID)
}

func (w Workspace) DerivedServerCore() string {
	if w.Topology == nil {
		return ""
	}

	primary, ok := w.Topology.PrimaryNodeData()
	if !ok {
		return ""
	}

	return string(primary.ID)
}

type ServerActivity struct {
	Active bool `json:"active"`
	Pid    int  `json:"pid"`
}
