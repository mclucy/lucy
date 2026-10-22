package cli

import (
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/workspace"
)

// liveStatusDocument keeps command output separate from workspace observation.
type liveStatusDocument struct {
	ServerDir   string                   `json:"root"`
	SavePath    string                   `json:"save_path"`
	PackageDirs []string                 `json:"package_dirs"`
	Packages    []liveStatusPackageView  `json:"packages"`
	Server      *workspace.ServerRuntime `json:"server,omitempty"`
	Activity    *liveStatusActivity      `json:"activity,omitempty"`
	Wrappers    workspace.WrapperInfo    `json:"wrappers"`
}

type liveStatusPackageView struct {
	ID           catalog.VersionedRef   `json:"ID"`
	Path         string                 `json:"Path"`
	Dependencies upstream.DependencySet `json:"Dependencies"`
}

type liveStatusActivity struct {
	Running bool `json:"active"`
}

func newLiveStatusDocument(observed workspace.Workspace) liveStatusDocument {
	document := liveStatusDocument{
		ServerDir:   observed.ServerDir,
		SavePath:    observed.SaveDir(),
		PackageDirs: observed.PackageDirs(),
		Packages:    make([]liveStatusPackageView, len(observed.Packages)),
		Wrappers:    observed.Wrappers,
	}
	for i, pkg := range observed.Packages {
		document.Packages[i] = liveStatusPackageView{
			ID:           pkg.ID,
			Path:         pkg.Path,
			Dependencies: pkg.Dependencies,
		}
	}
	if server := observed.Runtime(); server != nil {
		document.Server = server
		document.Activity = &liveStatusActivity{Running: observed.Running()}
	}
	return document
}
