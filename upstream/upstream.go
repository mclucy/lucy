// Package upstream defines the core upstream abstraction layer.
//
// Architecture overview:
//   - types.Source is a stable user-facing identifier (CLI/config/storage).
//   - Provider capabilities execute upstream operations.
//   - Source selection policy lives outside this package in a dedicated resolver
//     package under upstream (currently upstream/routing).
//
// Dependency inversion:
//   - This package defines interfaces and normalized conversion contracts.
//   - Concrete providers implement small capability interfaces and depend on
//     these contracts, not the other way around.
//   - Callers pass capability interfaces into Search/Info. Core logic depends
//     on abstractions rather than concrete upstream implementations.
//
// Boundary:
//   - upstream package executes provider capabilities and normalizes outputs.
//   - Source selection, source-auto policy, and multi-provider execution
//     strategies are handled by routing logic in subpackages.
package upstream

import (
	"fmt"
)

func Search(
	searcher Searcher,
	query Query,
) (res SearchResponse, err error) {
	res, err = searcher.Search(query)
	if err != nil {
		return res, err
	}
	if len(res.Items) == 0 {
		return res, fmt.Errorf("no projects found for \"%s\"", query.Keyword)
	}
	return res, nil
}
