package routing

import (
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
)

type SearchAggregateOptions struct {
	Enabled bool
}

// MaybeAggregateSearchResults is an optional post-processing utility. It is
// disabled by default and intentionally decoupled from SearchMany.
func MaybeAggregateSearchResults(
	results []upstream.SearchResponse,
	options SearchAggregateOptions,
) []upstream.SearchResponse {
	if !options.Enabled || len(results) <= 1 {
		return results
	}
	return []upstream.SearchResponse{AggregateSearchResults(results)}
}

// AggregateSearchResults merges multi-provider search results into one result.
// Source metadata remains in the original non-aggregated form and should be
// preferred unless aggregation is explicitly required by callers.
func AggregateSearchResults(results []upstream.SearchResponse) upstream.SearchResponse {
	aggregated := upstream.SearchResponse{
		Source: types.SourceAuto,
		Items:  make([]upstream.RemotePackageName, 0),
	}
	for _, res := range results {
		aggregated.Items = append(aggregated.Items, res.Items...)
	}
	return aggregated
}
