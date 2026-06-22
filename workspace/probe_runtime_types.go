package workspace

import (
	"os/exec"

	"github.com/mclucy/lucy/types"
)

type ServerRuntime struct {
	PrimaryEntrance   string                      `json:"primary_entrance"`
	GameVersion       types.BareVersion           `json:"game_version"`
	BootCommand       *exec.Cmd                   `json:"-"`
	Topology          *types.RuntimeTopology      `json:"-"`
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

	return runtimeIdentityPackage(e.RuntimeIdentities, node)
}

func (e *ServerRuntime) PrimaryRuntimeIdentity() *types.VersionedPackageRef {
	if e == nil {
		return nil
	}

	return primaryRuntimeIdentity(e.Topology, e.RuntimeIdentities)
}

func (e *ServerRuntime) DerivedLoaderVersion() string {
	if e == nil {
		return derivedLoaderVersion(nil, nil)
	}

	return derivedLoaderVersion(e.Topology, e.RuntimeIdentities)
}

func (e *ServerRuntime) DerivedModLoader() types.PlatformId {
	if e == nil {
		return derivedModLoader(nil)
	}

	return derivedModLoader(e.Topology)
}

func (e *ServerRuntime) DerivedServerCore() string {
	if e == nil {
		return derivedServerCore(nil)
	}

	return derivedServerCore(e.Topology)
}
