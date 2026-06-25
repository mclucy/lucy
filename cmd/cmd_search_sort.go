package cmd

import (
	"sort"

	"github.com/mclucy/lucy/upstream"
)

// SearchSort is the user-facing sort order for `lucy search` results.
// It is a consumer-side orchestration concept: the CLI applies it to the
// items already returned by providers, which always return their own default
// (relevance) order.
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

// applySearchSort reorders the items in each response according to sort.
// Relevance is a no-op (preserves the provider's order). Unrecognized sorts
// also fall through as no-op rather than failing, so a downstream bug never
// blocks the search result from being displayed.
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
