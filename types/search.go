package types

// SearchSort controls how providers rank search results.
type SearchSort string

const (
	SearchSortRelevance SearchSort = "relevance"
	SearchSortDownloads SearchSort = "downloads"
	SearchSortNewest    SearchSort = "newest"
	SearchSortName      SearchSort = "name"
)

func (s SearchSort) Valid() bool {
	switch s {
	case SearchSortRelevance, SearchSortDownloads, SearchSortNewest:
		return true
	default:
		return false
	}
}

type SearchOptions struct {
	IncludeClient  bool
	SortBy         SearchSort
	FilterPlatform Platform
}

type SearchResults struct {
	Source   Source
	Projects []ProjectName
}
