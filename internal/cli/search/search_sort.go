package search

import (
	"cmp"
	"sort"

	"github.com/mclucy/lucy/internal/cli"
	"github.com/mclucy/lucy/upstream"
)

// SearchSort is the user-facing sort order for `lucy search` results. It is
// applied consumer-side to items already returned by providers, which always
// return their own default (relevance) order.
type Sort string

const (
	SortRelevance Sort = "relevance"
	SortDownloads Sort = "downloads"
	SortNewest    Sort = "newest"
)

func (s Sort) Valid() bool {
	switch s {
	case SortRelevance, SortDownloads, SortNewest:
		return true
	}
	return false
}

// applySearchSort reorders items in each response according to s. Relevance
// preserves the provider's order, and unrecognized sorts also fall through as
// no-op rather than failing, so a bad sort never blocks the results.
func applySearchSort(results []upstream.SearchResponse, s Sort) {
	switch s {
	case "", SortRelevance:
		return
	case SortDownloads:
		for i := range results {
			items := results[i].Items
			sort.SliceStable(items, func(a, b int) bool {
				return cmp.Compare(items[b].Downloads, items[a].Downloads) < 0
			})
		}
	case SortNewest:
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
		{Value: string(SortRelevance), Description: "Sort by relevance"},
		{
			Value:       string(SortDownloads),
			Description: "Sort by download count",
		},
		{Value: string(SortNewest), Description: "Sort by newest"},
	}
}
