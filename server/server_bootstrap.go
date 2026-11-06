package server

import "github.com/mclucy/lucy/catalog"

// BootstrapMethod identifies how a locked server tree is prepared from its core
// artifact and inputs. The values are persisted in lucy-lock.yaml.
type BootstrapMethod string

const (
	// BootstrapMethodPlain copies the core artifact as server.jar.
	BootstrapMethodPlain BootstrapMethod = ""
	// BootstrapMethodPaperclip copies the core artifact and runs Paperclip in
	// patch-only mode.
	BootstrapMethodPaperclip BootstrapMethod = "paperclip"
	// BootstrapMethodFabric places the loader in the library layout and
	// generates launch files.
	BootstrapMethodFabric BootstrapMethod = "fabric"
	// BootstrapMethodForgeInstaller executes the pinned installer's processors.
	BootstrapMethodForgeInstaller BootstrapMethod = "forge-installer"
)

type BootstrapInput struct {
	Path             string `yaml:"path" json:"path"`
	catalog.Artifact `yaml:",inline"`
}
