package upstream

import "github.com/mclucy/lucy/types"

type Searcher interface {
	Search(q Query) (resp SearchResponse, err error)
}

type Query struct {
	Keyword        string
	SortBy         types.SearchSort
	ExcludeClient  bool
	FilterPlatform types.PlatformId
	Tags           []string
	Limit          int
}

type SearchResponse struct {
	// Source labels which upstream catalog produced this result set.
	// It is a semantic provenance marker, not a provider instance.
	Source   types.SourceId
	Items    []RemotePackageName
	Warnings []error
}

type Informer interface {
	Info(ref types.PackageRef) (info types.Metadata, err error)
}
