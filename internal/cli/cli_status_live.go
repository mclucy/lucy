package cli

import (
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/workspace"
)

// liveStatusDocument keeps command output separate from workspace observation.
type liveStatusDocument struct {
	ServerDir   string                  `json:"root"`
	SavePath    string                  `json:"save_path"`
	PackageDirs []string                `json:"package_dirs"`
	Packages    []liveStatusPackageView `json:"packages"`
	Server      *liveStatusServer       `json:"server,omitempty"`
	Activity    *liveStatusActivity     `json:"activity,omitempty"`
	Wrappers    workspace.WrapperInfo   `json:"wrappers"`
}

// liveStatusServer is the explicit CLI projection of the selected runtime.
// The field names are the public status wire format; the discovery domain
// keeps its own names.
type liveStatusServer struct {
	Primary     catalog.VersionedRef   `json:"primary_runtime"`
	PrimaryPath string                 `json:"primary_path,omitempty"`
	Components  []catalog.VersionedRef `json:"runtime_components"`
}

type liveStatusPackageView struct {
	ID           catalog.VersionedRef   `json:"ID"`
	Path         string                 `json:"Path"`
	Dependencies upstream.DependencySet `json:"Dependencies"`
}

type liveStatusActivity struct {
	Running bool `json:"active"`
}

func newLiveStatusDocument(observed *workspace.Workspace) liveStatusDocument {
	document := liveStatusDocument{
		ServerDir:   observed.ServerDir,
		SavePath:    observed.SaveDir(),
		PackageDirs: observed.PackageDirs(),
		Packages:    make([]liveStatusPackageView, len(observed.Packages)),
		Wrappers:    observed.Wrappers,
	}
	for i, pkg := range observed.Packages {
		document.Packages[i] = liveStatusPackageView{
			ID:   pkg.ID,
			Path: pkg.Path,
			Dependencies: upstream.DependencySet{
				Dependencies: pkg.Dependencies,
			},
		}
	}
	if selection := observed.Runtime(); selection != nil {
		document.Server = &liveStatusServer{
			Primary:    selection.Installation.Identity.Primary,
			Components: selectedRuntimeComponents(selection),
		}
		if selection.EntryPoint != nil {
			document.Server.PrimaryPath = selection.EntryPoint.Path
		}
		document.Activity = &liveStatusActivity{Running: observed.Running()}
	}
	return document
}
