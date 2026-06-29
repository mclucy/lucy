package install

import (
	"context"
	"fmt"

	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/upstream/routing"
)

type providerCandidateResolver struct {
	providers        []upstream.PackageSource
	rootProviders    map[string][]upstream.PackageSource
	rootProviderSet  map[string]struct{}
	defaultEcosystem types.Ecosystem
}

func (resolver providerCandidateResolver) ResolvePackage(
	ctx context.Context,
	id types.VersionedPackageRef,
) (types.ResolvedPackage, error) {
	attempts := []types.VersionedPackageRef{id}
	if id.Version == types.VersionCompatible {
		attempts = append(
			attempts,
			types.VersionedPackageRef{
				PackageRef: types.PackageRef{
					Eco:  id.Eco,
					Name: id.Name,
				},
				Version: types.VersionLatest,
			},
			types.VersionedPackageRef{
				PackageRef: types.PackageRef{
					Eco:  id.Eco,
					Name: id.Name,
				},
				Version: types.VersionAny,
			},
		)
	}

	var lastErrors []routing.ProviderError
	for _, attempt := range attempts {
		if err := ctx.Err(); err != nil {
			return types.ResolvedPackage{}, err
		}

		fetches, providerErrors := resolver.fetchMany(ctx, attempt)
		if len(fetches) == 0 {
			lastErrors = providerErrors
			continue
		}

		return fetches[0], nil
	}

	return types.ResolvedPackage{}, fmt.Errorf(
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

func (resolver providerCandidateResolver) fetchMany(
	ctx context.Context,
	id types.VersionedPackageRef,
) ([]types.ResolvedPackage, []routing.ProviderError) {
	providers := resolver.providersForPackage(id)
	if len(providers) == 0 {
		return nil, nil
	}

	groups := make([]providerFetchGroup, 0, len(providers))
	groupIndexes := map[string]int{}
	for _, provider := range providers {
		requestID, ok := resolver.requestIDForProvider(id, provider.Id())
		if !ok {
			continue
		}
		key := requestID.StringFull()
		if index, ok := groupIndexes[key]; ok {
			groups[index].providers = append(groups[index].providers, provider)
			continue
		}
		groupIndexes[key] = len(groups)
		groups = append(
			groups, providerFetchGroup{
				id:        requestID,
				providers: []upstream.PackageSource{provider},
			},
		)
	}

	results := make([]types.ResolvedPackage, 0, len(providers))
	providerErrors := make([]routing.ProviderError, 0)
	for _, group := range groups {
		if err := ctx.Err(); err != nil {
			return results, append(
				providerErrors,
				routing.ProviderError{Err: err},
			)
		}
		fetches, errors := routing.FetchMany(group.providers, group.id)
		results = append(results, fetches...)
		providerErrors = append(providerErrors, errors...)
	}
	return results, providerErrors
}

type providerFetchGroup struct {
	id        types.VersionedPackageRef
	providers []upstream.PackageSource
}

func (resolver providerCandidateResolver) requestIDForProvider(
	id types.VersionedPackageRef,
	source types.SourceId,
) (types.VersionedPackageRef, bool) {
	requestID := id
	switch source {
	case types.SourceMCDR:
		if id.Eco.IsModding() {
			return types.VersionedPackageRef{}, false
		}
		if id.Eco == types.EcoAny || id.Eco == types.EcoBare {
			requestID.Eco = types.EcoMcdr
		}
	case types.SourceModrinth, types.SourceCurseForge:
		if id.Eco == types.EcoMcdr {
			return types.VersionedPackageRef{}, false
		}
		if id.Eco == types.EcoAny && resolver.defaultEcosystem != types.EcoAny {
			requestID.Eco = resolver.defaultEcosystem
		}
	case types.SourceHangar, types.SourceSpiget:
		if id.Eco == types.EcoMcdr || id.Eco.IsModding() {
			return types.VersionedPackageRef{}, false
		}
		if id.Eco == types.EcoAny || id.Eco == types.EcoBare {
			requestID.Eco = types.EcoBukkit
		}
	}
	return requestID, true
}

func (resolver providerCandidateResolver) ResolveDependencies(
	ctx context.Context,
	pkg types.ResolvedPackage,
) ([]types.PackageDependencies, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	providers := providersForSource(resolver.providers, pkg.Id.Scope)
	dependencySets, providerErrors := routing.DependenciesMany(
		providers,
		versionedResolvedID(pkg),
	)
	if len(dependencySets) > 0 {
		return dependencySets, nil
	}

	return nil, fmt.Errorf(
		"install: failed to resolve mandatory dependency %s: %s",
		pkg.Id.PackageRef.StringBase(),
		formatProviderErrors(providerErrors),
	)
}

func providersForSource(
	providers []upstream.PackageSource,
	source types.SourceId,
) []upstream.PackageSource {
	if source == types.SourceUnknown {
		return providers
	}

	filtered := make([]upstream.PackageSource, 0, 1)
	for _, provider := range providers {
		if provider.Id() == source {
			filtered = append(filtered, provider)
		}
	}
	if len(filtered) == 0 {
		return providers
	}
	return filtered
}
