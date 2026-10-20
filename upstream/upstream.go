// Package upstream defines the core upstream abstraction layer.
//
// types.Provider identifies the execution layer that fetches metadata or
// artifacts; types.Registry identifies a fully functional package repository
// and is executed by exactly one provider. Provider capabilities implement the
// small interfaces in this package. Provider selection, unspecified-provider
// policy, and multi-provider execution strategies live outside this package in
// upstream/routing.
//
// Dependency inversion: this package defines the capability interfaces and
// normalized conversion contracts. Concrete providers implement those small
// interfaces and depend on these contracts, not the other way around.
// Callers pass capability interfaces into Search/Info.
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
