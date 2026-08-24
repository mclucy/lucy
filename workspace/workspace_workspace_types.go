package workspace

import "github.com/mclucy/lucy/types"

type Workspace struct {
	Root         string                    `json:"root"` // if found lucy on upper workspace
	SavePath     string                    `json:"save_path"`
	ModPath      []string                  `json:"mod_path"`
	Packages     []types.DiscoveredPackage `json:"packages"`
	Server       *ServerInstance           `json:"server,omitempty"`
	Activity     *ServerActivity           `json:"activity,omitempty"`
	Environments types.EnvironmentInfo     `json:"environments"`
}

type ServerActivity struct {
	Active bool `json:"active"`
	Pid    int  `json:"pid"`
}
