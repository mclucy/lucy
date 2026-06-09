package probe

import (
	"github.com/mclucy/lucy/types"
)

// Workspace components that do not exist, use an empty string. Note Runtime
// must exist, otherwise the program will exit; therefore, it is not a pointer.
type Workspace struct {
	Root         string                `json:"root"` // if found lucy on upperworkspace
	SavePath     string                `json:"save_path"`
	ModPath      []string              `json:"mod_path"`
	Packages     []types.Package       `json:"packages"`
	Runtime      *ServerRuntime        `json:"runtime,omitempty"`
	Activity     *types.ServerActivity `json:"activity,omitempty"`
	Environments types.EnvironmentInfo `json:"environments"`
	McdrRoot     string                `json:"mcdr_root"`
	LucyRoot     string                `json:"lucy_root"`
}
