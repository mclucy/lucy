package types

import (
	"os/exec"

	"github.com/mclucy/lucy/exttype"
)

// ServerInfo components that do not exist, use an empty string. Note Executable
// must exist, otherwise the program will exit; therefore, it is not a pointer.
type ServerInfo struct {
	WorkPath     string          `json:"work_path"`
	SavePath     string          `json:"save_path"`
	ModPath      []string        `json:"mod_path"`
	Packages     []Package       `json:"packages"`
	Executable   *ExecutableInfo `json:"executable,omitempty"`
	Activity     *ServerActivity `json:"activity,omitempty"`
	Environments EnvironmentInfo `json:"environments"`
}

type ExecutableInfo struct {
	Path              string           `json:"path"`
	GameVersion       RawVersion       `json:"game_version"`
	BootCommand       *exec.Cmd        `json:"-"`
	Topology          *RuntimeTopology `json:"topology,omitempty"`
	RuntimeIdentities []PackageId      `json:"runtime_identities,omitempty"`
	BridgeHints       []string         `json:"bridge_hints,omitempty"`
}

var UnknownExecutable = &ExecutableInfo{
	Path:        "",
	GameVersion: VersionUnknown,
	BootCommand: nil,
	Topology:    TopologyUnknown,
}

var NoExecutable = &ExecutableInfo{
	Path:        "",
	GameVersion: VersionNone,
	BootCommand: nil,
	Topology:    TopologyEmpty,
}

func (e *ExecutableInfo) IsValid() bool {
	return e != nil && e.Topology != nil
}

func (e *ExecutableInfo) Analyzable() bool {
	return e != nil && e.Topology != nil && len(e.RuntimeIdentities) > 0 && e != NoExecutable && e != UnknownExecutable
}

func (e *ExecutableInfo) RuntimeIdentityPackage(node *TopologyNode) *PackageId {
	if e == nil || node == nil {
		return nil
	}

	for i := range e.RuntimeIdentities {
		pkg := &e.RuntimeIdentities[i]
		if pkg.IdentityToPlatform() == node.IdentityPlatform {
			return pkg
		}
	}

	return nil
}

func (e *ExecutableInfo) PrimaryRuntimeIdentity() *PackageId {
	if e == nil || e.Topology == nil {
		return nil
	}

	primaryNode, ok := e.Topology.PrimaryNodeData()
	if !ok {
		return nil
	}

	return e.RuntimeIdentityPackage(&primaryNode)
}

func (e *ExecutableInfo) DerivedLoaderVersion() string {
	primaryIdentity := e.PrimaryRuntimeIdentity()
	if primaryIdentity == nil {
		return "unknown"
	}

	return primaryIdentity.Version.String()
}

func (e *ExecutableInfo) DerivedModLoader() Platform {
	if e == nil || e.Topology == nil {
		return PlatformNone
	}

	primary, ok := e.Topology.PrimaryNodeData()
	if !ok {
		return PlatformNone
	}

	if !primary.IdentityPlatform.Valid() {
		return PlatformNone
	}

	return primary.IdentityPlatform
}

type ServerActivity struct {
	Active bool `json:"active"`
	Pid    int  `json:"pid"`
}

type EnvironmentInfo struct {
	Lucy *LucyEnv `json:"lucy,omitempty"`
	Mcdr *McdrEnv `json:"mcdr,omitempty"`
}

type McdrEnv struct {
	Version RawVersion              `json:"version"`
	Config  *exttype.FileMcdrConfig `json:"config,omitempty"`
}

// LucyEnv is a placeholder for Lucy environment; currently just a boolean
// indicating presence, but can be expanded with more details if needed
type LucyEnv bool
