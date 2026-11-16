// Package loadermeta resolves the loader metadata that Fabric and Quilt
// publish in the same shape: loader and game version lists, and a server
// profile that names Maven libraries.
package loadermeta

import (
	"fmt"
	"path"
	"strings"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/internal/maven"
	"github.com/mclucy/lucy/version"
)

// Definition is the per-loader difference the shared meta client needs.
type Definition struct {
	// APIBase is the meta API origin, without a trailing slash.
	APIBase string
	// APIVersion is the API version segment the endpoints share.
	APIVersion string
	// MavenBase is the repository base URL of the loader artifact.
	MavenBase string
	// Coordinate is the Maven group and artifact of the loader.
	Coordinate string
	// Provider identifies the loader as an artifact source.
	Provider catalog.Provider
	// ProjectID is the artifact project name.
	ProjectID string
	// StableFlag reports whether loader entries carry a stable boolean. A
	// loader without it encodes prereleases in the version suffix.
	StableFlag bool
}

// Provider resolves one loader's version lists, server profile, and Maven
// artifacts over the shared network cache.
type Provider struct {
	network    *cache.Client
	definition Definition
}

// New constructs a loader meta provider. network supplies the cached HTTP
// transport; construction performs no requests.
func New(network *cache.Client, definition Definition) *Provider {
	return &Provider{network: network, definition: definition}
}

// LoaderVersion is the loader entry a selector resolved to.
type LoaderVersion struct {
	Version version.Version
	Maven   string
	Hashes  map[string]string
}

// loaderVersionEntry is one entry of a loader version list. Quilt publishes
// the Maven coordinate and hashes of its loader; Fabric derives both from the
// definition.
type loaderVersionEntry struct {
	Version string            `json:"version"`
	Stable  bool              `json:"stable"`
	Maven   string            `json:"maven"`
	Hashes  map[string]string `json:"hashes"`
}

type gameVersionEntry struct {
	Version string `json:"version"`
	Stable  bool   `json:"stable"`
}

// Profile is the launch profile of one game/loader pair.
type Profile struct {
	Libraries []Library `json:"libraries"`
}

// Library is one Maven library a profile names. URL is the repository base,
// not the artifact URL.
type Library struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// LoaderArtifact pins a resolved loader entry to its Maven repository. The
// entry's published hashes are copied; the repository sidecar is added by the
// caller.
func (p *Provider) LoaderArtifact(
	selected LoaderVersion,
) (catalog.Artifact, error) {
	coordinate := selected.Maven
	if coordinate == "" {
		coordinate = p.definition.Coordinate + ":" +
			selected.Version.String()
	}
	repositoryPath, err := maven.Path(coordinate)
	if err != nil {
		return catalog.Artifact{}, fmt.Errorf(
			"invalid %s Maven coordinate %q: %w",
			p.definition.ProjectID,
			coordinate,
			err,
		)
	}
	return catalog.Artifact{
		Provider:  p.definition.Provider,
		ProjectID: p.definition.ProjectID,
		Version:   selected.Version,
		Filename:  path.Base(repositoryPath),
		URL:       p.definition.MavenBase + repositoryPath,
		Hashes:    cloneHashes(selected.Hashes),
	}, nil
}

// IsLoaderLibrary reports whether a profile coordinate is the loader itself,
// which bootstrap installs from the core artifact.
func (p *Provider) IsLoaderLibrary(coordinate string) bool {
	base, _, _ := strings.Cut(coordinate, "@")
	parts := strings.Split(base, ":")
	return len(parts) >= 2 &&
		parts[0]+":"+parts[1] == p.definition.Coordinate
}

func cloneHashes(hashes map[string]string) map[string]string {
	clone := make(map[string]string, len(hashes))
	for name, value := range hashes {
		clone[name] = value
	}
	return clone
}
