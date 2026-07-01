package workspace

import (
	"os/exec"

	"github.com/mclucy/lucy/types"
)

type ServerInstance struct {
	PrimaryEntrance string                `json:"primary_entrance"`
	GameVersion     types.BareVersion     `json:"game_version"`
	BootCommand     *exec.Cmd             `json:"-"`
	topology        *types.ServerTopology `json:"-"`
}

var UnknownExecutable = &ServerInstance{
	PrimaryEntrance: "",
	GameVersion:     types.VersionUnknown,
	BootCommand:     nil,
	topology:        types.TopologyUnknown,
}

var NoExecutable = &ServerInstance{
	PrimaryEntrance: "",
	GameVersion:     types.VersionNone,
	BootCommand:     nil,
	topology:        types.TopologyEmpty,
}

func (e *ServerInstance) IsValid() bool {
	return e != nil && e.topology != nil
}

func (e *ServerInstance) Analyzable() bool {
	return e != nil && e.topology != nil && len(e.topology.AllIdentities()) > 0 && e != NoExecutable && e != UnknownExecutable
}
