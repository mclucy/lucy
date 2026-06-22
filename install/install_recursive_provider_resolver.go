package install

import (
	"context"
	"fmt"

	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/upstream/routing"
)

type providerCandidateResolver struct {
	providers       []upstream.PackageSource
	rootProviders   map[string][]upstream.PackageSource
	rootProviderSet map[string]struct{}
}

func (resolver providerCandidateResolver) ResolvePackage(
	ctx context.Context,
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
		if err := ctx.Err(); err != nil {
			return types.Package{}, err
		}

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
		remote := types.PackageRemote{
			Source:        fetch.Id.Scope,
			FileUrl:       fetch.FileUrl,
			Filename:      fetch.Filename,
			Hash:          fetch.Hash,
			HashAlgorithm: fetch.HashAlgorithm,
		}
		return types.Package{
			Id: types.VersionedPackageRef{
				PackageRef: fetch.Id.PackageRef,
				Version:    fetch.Id.Version,
			},
			Remote: &remote,
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
) []upstream.PackageSource {
	key := id.StringBase()
	if _, ok := resolver.rootProviderSet[key]; ok {
		if providers := resolver.rootProviders[key]; len(providers) > 0 {
			return providers
		}
	}
	return resolver.providers
}

func (resolver providerCandidateResolver) ResolveDependencies(
	ctx context.Context,
	pkg types.Package,
) ([]types.PackageDependencies, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

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
	providers []upstream.PackageSource,
	remote *types.PackageRemote,
) []upstream.PackageSource {
	if remote == nil {
		return providers
	}

	filtered := make([]upstream.PackageSource, 0, 1)
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
