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
	Path          string           `json:"path"`
	GameVersion   RawVersion       `json:"game_version"`
	ModLoader     Platform         `json:"mod_loader"`
	LoaderVersion RawVersion       `json:"loader_version"`
	BootCommand   *exec.Cmd        `json:"-"`
	Topology      *RuntimeTopology `json:"topology,omitempty"`
	BridgeHints   []string         `json:"bridge_hints,omitempty"`
}

func (e *ExecutableInfo) IsValid() bool {
	return e.Path != "" && e.GameVersion != "" && e.ModLoader.Valid()
}

// DerivedModLoader returns the platform representing the primary mod loader.
// If Topology is set and resolved, it derives the value from the primary node's
// IdentityPlatform. Otherwise it returns the legacy ModLoader field directly.
func (e *ExecutableInfo) DerivedModLoader() Platform {
	if e == nil {
		return PlatformNone
	}
	if e.Topology != nil && e.Topology.Resolved() {
		if primary, ok := e.Topology.PrimaryNodeData(); ok {
			if primary.IdentityPlatform.Valid() {
				return primary.IdentityPlatform
			}
		}
	}
	return e.ModLoader
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
