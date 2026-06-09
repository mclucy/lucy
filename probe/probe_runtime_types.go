package probe

import (
	"os/exec"

	"github.com/mclucy/lucy/types"
)

type ServerRuntime struct {
	PrimaryEntrance   string                      `json:"primary_entrance"`
	GameVersion       types.BareVersion           `json:"game_version"`
	BootCommand       *exec.Cmd                   `json:"-"`
	Topology          *types.RuntimeTopology      `json:"topology,omitempty"`
	RuntimeIdentities []types.VersionedPackageRef `json:"runtime_identities,omitempty"`
	BridgeHints       []string                    `json:"bridge_hints,omitempty"`
}

var UnknownExecutable = &ServerRuntime{
	PrimaryEntrance: "",
	GameVersion:     types.VersionUnknown,
	BootCommand:     nil,
	Topology:        types.TopologyUnknown,
}

var NoExecutable = &ServerRuntime{
	PrimaryEntrance: "",
	GameVersion:     types.VersionNone,
	BootCommand:     nil,
	Topology:        types.TopologyEmpty,
}

func (e *ServerRuntime) IsValid() bool {
	return e != nil && e.Topology != nil
}

func (e *ServerRuntime) Analyzable() bool {
	return e != nil && e.Topology != nil && len(e.RuntimeIdentities) > 0 && e != NoExecutable && e != UnknownExecutable
}

func (e *ServerRuntime) RuntimeIdentityPackage(node *types.TopologyNode) *types.VersionedPackageRef {
	if e == nil || node == nil {
		return nil
	}

	for i := range e.RuntimeIdentities {
		pkg := &e.RuntimeIdentities[i]
		if string(pkg.Name) == string(node.ID) {
			return pkg
		}
	}

	return nil
}

func (e *ServerRuntime) PrimaryRuntimeIdentity() *types.VersionedPackageRef {
	if e == nil || e.Topology == nil {
		return nil
	}

	primaryNode, ok := e.Topology.PrimaryNodeData()
	if !ok {
		return nil
	}

	return e.RuntimeIdentityPackage(&primaryNode)
}

func (e *ServerRuntime) DerivedLoaderVersion() string {
	primaryIdentity := e.PrimaryRuntimeIdentity()
	if primaryIdentity == nil {
		return "unknown"
	}

	return primaryIdentity.Version.String()
}

func (e *ServerRuntime) DerivedModLoader() types.PlatformId {
	if e == nil || e.Topology == nil {
		return types.PlatformNone
	}

	primary, ok := e.Topology.PrimaryNodeData()
	if !ok {
		return types.PlatformNone
	}

	return types.DeclaredModdingPlatformForNode(primary.ID)
}

func (e *ServerRuntime) DerivedServerCore() string {
	if e == nil || e.Topology == nil {
		return ""
	}

	primary, ok := e.Topology.PrimaryNodeData()
	if !ok {
		return ""
	}

	return string(primary.ID)
}
