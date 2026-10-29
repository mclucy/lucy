package cli

import (
	"errors"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/filetrack"
	"github.com/mclucy/lucy/javainstall"
	"github.com/mclucy/lucy/operation"
	"github.com/mclucy/lucy/serverregistry"
	"github.com/mclucy/lucy/upstream/routing"
)

// Dependencies is the concrete service assembly one invocation uses. The root
// wiring fills it after server selection, so services observe the selected
// workspace rather than the process working directory. Every field is an
// explicitly typed service; commands read only the fields they declared in
// their constructor.
type Dependencies struct {
	// Operations runs workspace operations through the configured installer.
	Operations *operation.Service
	// Files tracks and restores files in the selected workspace.
	Files *filetrack.Service
	// Router answers upstream metadata requests.
	Router *routing.Router
	// Network fetches and caches upstream responses.
	Network *cache.Client
	// Registry resolves registered server names and paths. RegistryError
	// reports why the registry could not be constructed; commands that need
	// the registry return it instead of dereferencing a nil Registry.
	Registry      *serverregistry.Registry
	RegistryError error
	// JavaHost plans and executes Java installations for this invocation.
	JavaHost *javainstall.Host
}

// ServerRegistry returns the invocation's registry or the reason it is
// unavailable. Commands that need the registry call this instead of
// dereferencing Registry directly.
func (d *Dependencies) ServerRegistry() (*serverregistry.Registry, error) {
	if d.Registry != nil {
		return d.Registry, nil
	}
	if d.RegistryError != nil {
		return nil, d.RegistryError
	}
	return nil, errors.New("server registry is unavailable")
}
