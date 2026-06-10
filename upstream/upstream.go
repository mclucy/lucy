// Package upstream defines the core upstream abstraction layer.
//
// Architecture overview:
//   - types.Source is a stable user-facing identifier (CLI/config/storage).
//   - Provider is a behavior interface that executes upstream operations.
//   - Source selection policy lives outside this package in a dedicated resolver
//     package under upstream (currently upstream/routing).
//
// Dependency inversion:
//   - This package defines interfaces and normalized conversion contracts.
//   - Concrete providers (modrinth, mcdr, curseforge, githubsource) implement
//     Provider and depend on these contracts, not the other way around.
//   - Callers pass Provider into Fetch/Search/Info. Core logic depends on
//     abstractions rather than concrete upstream implementations.
//
// Boundary:
//   - upstream package executes provider capabilities and normalizes outputs.
//   - Source selection, source-auto policy, and multi-provider execution
//     strategies are handled by routing logic in subpackages.
package upstream

import (
	"fmt"

	"github.com/mclucy/lucy/types"
)

// IoC via dependency injection

func Fetch(
	provider Provider,
	resolver VersionSelectorResolver,
	id types.VersionedPackageRef,
) (result FetchResult, err error) {
	resolvedID, err := resolver.ResolveVersionSelector(id)
	if err != nil {
		return FetchResult{}, err
	}

	raw, err := provider.Fetch(resolvedID)
	if err != nil {
		return FetchResult{}, err
	}
	result.ResolvedID = resolvedID
	result.Remote = raw.ToPackageRemote()
	return result, nil
}

func Dependencies(
	provider Provider,
	id types.VersionedPackageRef,
) (deps *types.PackageDependencies, err error) {
	raw, err := provider.Dependencies(id)
	if err != nil {
		return nil, err
	}
	result := raw.ToPackageDependencies()
	return &result, nil
}

func Info(
	informer Informer,
	ref types.PackageRef,
) (info types.Metadata, err error) {
	return informer.Info(ref)
}

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
