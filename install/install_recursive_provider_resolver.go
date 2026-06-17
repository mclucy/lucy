package install

import (
	"fmt"

	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/upstream/routing"
)

type providerCandidateResolver struct {
	providers       []upstream.Provider
	rootProviders   map[string][]upstream.Provider
	rootProviderSet map[string]struct{}
}

func (resolver providerCandidateResolver) ResolvePackage(
	id types.VersionedPackageRef,
) (types.Package, error) {
	attempts := []types.VersionedPackageRef{id}
	if id.Version == types.VersionCompatible {
		attempts = append(
			attempts,
			types.VersionedPackageRef{
				PackageRef: types.PackageRef{
					Platform: id.Platform,
					Name:     id.Name,
				},
				Version: types.VersionLatest,
			},
			types.VersionedPackageRef{
				PackageRef: types.PackageRef{
					Platform: id.Platform,
					Name:     id.Name,
				},
				Version: types.VersionAny,
			},
		)
	}

	var lastErrors []routing.ProviderError
	for _, attempt := range attempts {
		providers := resolver.providersForPackage(attempt)
		fetches, providerErrors := routing.FetchMany(
			providers,
			attempt,
		)
		if len(fetches) == 0 {
			lastErrors = providerErrors
			continue
		}

		fetch := fetches[0]
		return types.Package{
			Id:     fetch.ResolvedID,
			Remote: &fetch.Remote,
		}, nil
	}

	return types.Package{}, fmt.Errorf(
		"install: failed to resolve mandatory dependency %s: %s",
		id.StringBase(),
		formatProviderErrors(lastErrors),
	)
}

func (resolver providerCandidateResolver) providersForPackage(
	id types.VersionedPackageRef,
) []upstream.Provider {
	key := id.StringBase()
	if _, ok := resolver.rootProviderSet[key]; ok {
		if providers := resolver.rootProviders[key]; len(providers) > 0 {
			return providers
		}
	}
	return resolver.providers
}

func (resolver providerCandidateResolver) ResolveDependencies(
	pkg types.Package,
) ([]types.PackageDependencies, error) {
	providers := providersForSource(resolver.providers, pkg.Remote)
	dependencySets, providerErrors := routing.DependenciesMany(
		providers,
		pkg.Id,
	)
	if len(dependencySets) > 0 {
		return dependencySets, nil
	}

	return nil, fmt.Errorf(
		"install: failed to resolve mandatory dependency %s: %s",
		pkg.Id.StringBase(),
		formatProviderErrors(providerErrors),
	)
}

func providersForSource(
	providers []upstream.Provider,
	remote *types.PackageRemote,
) []upstream.Provider {
	if remote == nil {
		return providers
	}

	filtered := make([]upstream.Provider, 0, 1)
	for _, provider := range providers {
		if provider.Id() == remote.Source {
			filtered = append(filtered, provider)
		}
	}
	if len(filtered) == 0 {
		return providers
	}
	return filtered
}
