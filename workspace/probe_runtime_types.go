package workspace

import (
	"os/exec"

	"github.com/mclucy/lucy/types"
)

type ServerRuntime struct {
	PrimaryEntrance string                 `json:"primary_entrance"`
	GameVersion     types.BareVersion      `json:"game_version"`
	BootCommand     *exec.Cmd              `json:"-"`
	topology        *types.RuntimeTopology `json:"-"`
	BridgeHints     []string               `json:"bridge_hints,omitempty"`
}

var UnknownExecutable = &ServerRuntime{
	PrimaryEntrance: "",
	GameVersion:     types.VersionUnknown,
	BootCommand:     nil,
	topology:        types.TopologyUnknown,
}

var NoExecutable = &ServerRuntime{
	PrimaryEntrance: "",
	GameVersion:     types.VersionNone,
	BootCommand:     nil,
	topology:        types.TopologyEmpty,
}

func (e *ServerRuntime) IsValid() bool {
	return e != nil && e.topology != nil
}

func (e *ServerRuntime) Analyzable() bool {
	return e != nil && e.topology != nil && len(e.topology.AllIdentities()) > 0 && e != NoExecutable && e != UnknownExecutable
}
