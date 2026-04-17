package types

import (
	"os/exec"

	"github.com/mclucy/lucy/exttype"
)

// ServerInfo components that do not exist, use an empty string. Note Runtime
// must exist, otherwise the program will exit; therefore, it is not a pointer.
type ServerInfo struct {
	WorkPath     string          `json:"work_path"`
	SavePath     string          `json:"save_path"`
	ModPath      []string        `json:"mod_path"`
	Packages     []Package       `json:"packages"`
	Runtime      *RuntimeInfo    `json:"runtime,omitempty"`
	Activity     *ServerActivity `json:"activity,omitempty"`
	Environments EnvironmentInfo `json:"environments"`
}

type RuntimeInfo struct {
	PrimaryEntrance   string           `json:"primary_entrance"`
	GameVersion       RawVersion       `json:"game_version"`
	BootCommand       *exec.Cmd        `json:"-"`
	Topology          *RuntimeTopology `json:"topology,omitempty"`
	RuntimeIdentities []PackageId      `json:"runtime_identities,omitempty"`
	BridgeHints       []string         `json:"bridge_hints,omitempty"`
}

var UnknownExecutable = &RuntimeInfo{
	PrimaryEntrance: "",
	GameVersion:     VersionUnknown,
	BootCommand:     nil,
	Topology:        TopologyUnknown,
}

var NoExecutable = &RuntimeInfo{
	PrimaryEntrance: "",
	GameVersion:     VersionNone,
	BootCommand:     nil,
	Topology:        TopologyEmpty,
}

func (e *RuntimeInfo) IsValid() bool {
	return e != nil && e.Topology != nil
}

func (e *RuntimeInfo) Analyzable() bool {
	return e != nil && e.Topology != nil && len(e.RuntimeIdentities) > 0 && e != NoExecutable && e != UnknownExecutable
}

func (e *RuntimeInfo) RuntimeIdentityPackage(node *TopologyNode) *PackageId {
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

func (e *RuntimeInfo) PrimaryRuntimeIdentity() *PackageId {
	if e == nil || e.Topology == nil {
		return nil
	}

	primaryNode, ok := e.Topology.PrimaryNodeData()
	if !ok {
		return nil
	}

	return e.RuntimeIdentityPackage(&primaryNode)
}

func (e *RuntimeInfo) DerivedLoaderVersion() string {
	primaryIdentity := e.PrimaryRuntimeIdentity()
	if primaryIdentity == nil {
		return "unknown"
	}

	return primaryIdentity.Version.String()
}

func (e *RuntimeInfo) DerivedModLoader() Platform {
	if e == nil || e.Topology == nil {
		return PlatformNone
	}

	primary, ok := e.Topology.PrimaryNodeData()
	if !ok {
		return PlatformNone
	}

	return DeclaredModdingPlatformForNode(primary.ID)
}

func (e *RuntimeInfo) DerivedServerCore() string {
	if e == nil || e.Topology == nil {
		return ""
	}

	primary, ok := e.Topology.PrimaryNodeData()
	if !ok {
		return ""
	}

	return string(primary.ID)
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
