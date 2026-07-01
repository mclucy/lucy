package workspace

import (
	"github.com/mclucy/lucy/types"
)

type ServerInstance struct {
	PrimaryEntrance     string                      `json:"primary_entrance"`
	GameVersion         types.BareVersion           `json:"game_version"`
	PrimaryEcosystems   []types.Ecosystem           `json:"primaryecosystems,omitempty"`
	SecondaryEcosystems []types.Ecosystem           `json:"secondary_ecosystems,omitempty"`
	Identities          []types.VersionedPackageRef `json:"identities,omitempty"`
	topology            *types.ServerTopology       `json:"-"`
}

var UnknownExecutable = &ServerInstance{
	PrimaryEntrance: "",
	GameVersion:     types.VersionUnknown,
	topology:        types.TopologyUnknown,
}

var NoExecutable = &ServerInstance{
	PrimaryEntrance: "",
	GameVersion:     types.VersionNone,
	topology:        types.TopologyEmpty,
}

func (e *ServerInstance) IsValid() bool {
	return e != nil && e.topology != nil
}
