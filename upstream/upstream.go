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
//   - Callers pass Provider into Fetch/Search/Information. Core logic depends on
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
	id types.PackageId,
) (result FetchResult, err error) {
	resolvedID, err := provider.ParseAmbiguousId(id)
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
	id types.PackageId,
) (deps *types.PackageDependencies, err error) {
	raw, err := provider.Dependencies(id)
	if err != nil {
		return nil, err
	}
	result := raw.ToPackageDependencies()
	return &result, nil
}

func PlatformSupport(src types.Source, name types.PackageName) (
	supports *types.PlatformSupport,
	err error,
) {
	// TODO: Implement
	panic("not implemented")
}

func Metadata(
	provider Provider,
	name types.PackageName,
) (info types.Metadata, err error) {
	raw, err := provider.Metadata(name)
	if err != nil {
		return types.Metadata{}, err
	}
	info = raw.ToProjectInformation()
	info.From = provider.Source()
	return info, nil
}

func Search(
	provider Provider,
	query types.PackageName,
	option types.SearchOptions,
) (res types.SearchResults, err error) {
	raw, err := provider.Search(string(query), option)
	if err != nil {
		return res, err
	}
	res = raw.ToSearchResults()
	if len(res.Projects) == 0 {
		return res, fmt.Errorf("no projects found for \"%s\"", query)
	}
	return res, nil
}

// InferVersion replaces inferable version constants with their inferred versions
// through sources. You should call this function before parsing the version to
// ComparableVersion.
//
// TODO: Remove, infer version should not be exposed. All inference will be done in providers.
func InferVersion(
	provider Provider,
	id types.PackageId,
) (infer types.PackageId) {
	return id
}
