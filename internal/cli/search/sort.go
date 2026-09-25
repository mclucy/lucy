package search

import (
	"sort"

	"github.com/mclucy/lucy/internal/cli"
	"github.com/mclucy/lucy/upstream"
)

// SearchSort is the user-facing sort order for `lucy search` results. It is
// applied consumer-side to items already returned by providers, which always
// return their own default (relevance) order.
type SearchSort string

const (
	SearchSortRelevance SearchSort = "relevance"
	SearchSortDownloads SearchSort = "downloads"
	SearchSortNewest    SearchSort = "newest"
)

func (s SearchSort) Valid() bool {
	switch s {
	case SearchSortRelevance, SearchSortDownloads, SearchSortNewest:
		return true
	}
	return false
}

// applySearchSort reorders items in each response according to s. Relevance
// preserves the provider's order, and unrecognized sorts also fall through as
// no-op rather than failing, so a bad sort never blocks the results.
func applySearchSort(results []upstream.SearchResponse, s SearchSort) {
	switch s {
	case "", SearchSortRelevance:
		return
	case SearchSortDownloads:
		for i := range results {
			items := results[i].Items
			sort.SliceStable(items, func(a, b int) bool {
				return items[a].Downloads > items[b].Downloads
			})
		}
	case SearchSortNewest:
		for i := range results {
			items := results[i].Items
			sort.SliceStable(items, func(a, b int) bool {
				return items[a].LastUpdated.After(items[b].LastUpdated)
			})
		}
	}
}

func StaticSortCandidates() []cli.CompletionCandidate {
	return []cli.CompletionCandidate{
		{Value: string(SearchSortRelevance), Description: "Sort by relevance"},
		{
			Value:       string(SearchSortDownloads),
			Description: "Sort by download count",
		},
		{Value: string(SearchSortNewest), Description: "Sort by newest"},
	}
}
