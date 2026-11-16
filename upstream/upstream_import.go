package upstream

import (
	"context"

	"github.com/mclucy/lucy/catalog"
)

// ImportPack is a downloadable server pack resolved from a registry project.
type ImportPack struct {
	Provider catalog.Provider
	Project  string
	Name     string
	Version  string
	Filename string
	URL      string
	Hashes   map[string]string
}

// ImportPackResolver resolves a registry project and optional version selector
// into a downloadable pack. Implementations return an error when the project
// has no server pack.
type ImportPackResolver interface {
	ResolveImportPack(
		ctx context.Context,
		project, version string,
	) (ImportPack, error)
}
