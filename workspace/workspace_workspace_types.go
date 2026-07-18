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

func (w Workspace) PrimaryRuntimeIdentity() *types.VersionedPackageRef {
	if w.Server == nil || w.Server.PrimaryRuntime == nil {
		return nil
	}
	return &w.Server.PrimaryRuntime.Identity
}

func (w Workspace) DerivedModLoader() types.Ecosystem {
	if w.Server == nil {
		return types.EcoUnspecified
	}
	return w.Server.DerivedModLoader()
}

func (w Workspace) DerivedServerCore() string {
	if w.Server == nil {
		return ""
	}
	return w.Server.DerivedServerCore()
}

type ServerActivity struct {
	Active bool `json:"active"`
	Pid    int  `json:"pid"`
}
