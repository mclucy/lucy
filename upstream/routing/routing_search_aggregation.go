package routing

import "github.com/mclucy/lucy/types"

type SearchAggregateOptions struct {
	Enabled bool
}

// MaybeAggregateSearchResults is an optional post-processing utility. It is
// disabled by default and intentionally decoupled from SearchMany.
func MaybeAggregateSearchResults(
	results []types.SearchResults,
	options SearchAggregateOptions,
) []types.SearchResults {
	if !options.Enabled || len(results) <= 1 {
		return results
	}
	return []types.SearchResults{AggregateSearchResults(results)}
}

// AggregateSearchResults merges multi-provider search results into one result.
// Source metadata remains in the original non-aggregated form and should be
// preferred unless aggregation is explicitly required by callers.
func AggregateSearchResults(results []types.SearchResults) types.SearchResults {
	aggregated := types.SearchResults{
		Source:   types.SourceAuto,
		Projects: make([]types.ProjectName, 0),
	}
	for _, res := range results {
		aggregated.Projects = append(aggregated.Projects, res.Projects...)
	}
	return aggregated
}
