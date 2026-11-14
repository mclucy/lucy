package upstream

import (
	"context"
	"crypto/sha1"
	"strings"
	"time"

	"github.com/mclucy/lucy/catalog"
)

// ProviderIdentifier exposes the execution-layer identity of a provider
// capability.
type ProviderIdentifier interface {
	Provider() catalog.Provider
}

type SearchProvider interface {
	ProviderIdentifier
	Searcher
}

// PackageCatalogue lists the published builds of one repository and the
// candidates it holds for an explicit project identity.
type PackageCatalogue interface {
	ProviderIdentifier
	Builds(ctx context.Context, project string) ([]Build, error)
	Candidates(
		ctx context.Context,
		project, minecraft string,
		loader catalog.Ecosystem,
	) ([]Candidate, error)
}

type InfoProvider interface {
	ProviderIdentifier
	Informer
}

type ArtifactFingerprint interface {
	SHA1() ([sha1.Size]byte, error)
	MurmurHash() (uint32, error)
}

type Searcher interface {
	Search(ctx context.Context, q Query) (resp SearchResponse, err error)
}

type Query struct {
	Keyword         string
	IncludeClient   bool
	FilterEcosystem catalog.Ecosystem
	Tags            []string
	Limit           int
}

type SearchResponse struct {
	// Provider labels which upstream provider produced this result set.
	Provider catalog.Provider `json:"provider"`
	Items    []SearchResult   `json:"items"`
	Warnings []error          `json:"warnings,omitempty"`
}

type Informer interface {
	Info(ctx context.Context, ref catalog.Ref) (info catalog.Metadata, err error)
}

// Info is a project description from one upstream. Ref identifies the
// upstream project that answered. Metadata is the content it returned.
type Info struct {
	Ref      catalog.Ref
	Metadata catalog.Metadata
}

// SearchResult represents a single search result with optional metadata.
// Providers populate what their API returns; display layer handles zero values.
type SearchResult struct {
	ProjectName string           `json:"project"`
	Provider    catalog.Provider `json:"provider"`

	// Optional metadata — zero value means unavailable.
	Title       string    `json:"title,omitempty"`
	Description string    `json:"description,omitempty"`
	Downloads   int64     `json:"downloads,omitempty"`
	LastUpdated time.Time `json:"last_updated,omitzero"`
}

func (r SearchResult) DisplayName() string {
	if r.Title != "" {
		return r.Title
	}
	if r.Provider == catalog.ProviderMCDR {
		return strings.ReplaceAll(r.ProjectName, "_", "-")
	}
	return r.ProjectName
}
