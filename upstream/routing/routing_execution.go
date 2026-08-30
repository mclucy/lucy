package routing

import (
	"errors"
	"fmt"
	"sync"

	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
)

var ErrNoProviderSucceeded = errors.New("no provider succeeded")

type ProviderError struct {
	// Source identifies the semantic upstream label for user-facing diagnostics.
	// The failed runtime executor is a Provider implementation.
	Source types.SourceId
	Err    error
}

func (e ProviderError) Error() string {
	return fmt.Sprintf("%s: %v", e.Source.String(), e.Err)
}

func (e ProviderError) Unwrap() error {
	return e.Err
}

// SearchMany executes search on all providers in parallel.
//
// Default behavior is non-aggregated: each provider contributes one
// upstream.SearchResponse item in the returned slice.
func SearchMany(
	providers []upstream.SearchSource,
	query string,
	options upstream.SearchOptions,
) ([]upstream.SearchResponse, []ProviderError) {
	if len(providers) == 0 {
		return nil, nil
	}

	type slot struct {
		res    upstream.SearchResponse
		err    ProviderError
		ok     bool
		failed bool
	}

	slots := make([]slot, len(providers))
	var wg sync.WaitGroup

	for i, provider := range providers {
		wg.Go(func() {
			res, err := upstream.Search(
				provider,
				upstream.Query{
					Keyword:         query,
					ExcludeClient:   !options.IncludeClient,
					FilterEcosystem: options.FilterEcosystem,
				},
			)
			if err != nil {
				slots[i] = slot{
					failed: true,
					err: ProviderError{
						Source: provider.Id(),
						Err:    err,
					},
				}
				return
			}
			if res.Source == types.SourceUnknown {
				res.Source = provider.Id()
			}
			slots[i] = slot{ok: true, res: res}
		})
	}

	wg.Wait()

	results := make([]upstream.SearchResponse, 0, len(providers))
	providerErrors := make([]ProviderError, 0)
	for _, item := range slots {
		if item.ok {
			results = append(results, item.res)
		}
		if item.failed {
			providerErrors = append(providerErrors, item.err)
		}
	}

	return results, providerErrors
}

// FetchMany executes fetch on all providers in parallel and returns all
// successful results.
func FetchMany(
	providers []upstream.PackageSource,
	id types.VersionedPackageRef,
) ([]types.ResolvedPackage, []ProviderError) {
	if len(providers) == 0 {
		return nil, nil
	}

	type slot struct {
		res    types.ResolvedPackage
		err    ProviderError
		ok     bool
		failed bool
	}

	slots := make([]slot, len(providers))
	var wg sync.WaitGroup

	for i, provider := range providers {
		wg.Add(1)
		go func(index int, provider upstream.PackageSource) {
			defer wg.Done()
			resolvedID, err := provider.ResolveVersionSelector(id)
			if err != nil {
				slots[index] = slot{
					failed: true,
					err: ProviderError{
						Source: provider.Id(),
						Err:    err,
					},
				}
				return
			}

			remoteData, err := provider.Fetch(resolvedID)
			if err != nil {
				slots[index] = slot{
					failed: true,
					err: ProviderError{
						Source: provider.Id(),
						Err:    err,
					},
				}
				return
			}
			// Providers receive a VersionedPackageRef (no scope) and thus
			// cannot fill Id.Scope themselves. Routing supplies the scope
			// from the provider identity, and the platform/name/version
			// from the resolvedID returned by ResolveVersionSelector.
			remoteData.Id = types.FullPackageRef{
				PackageRef: resolvedID.PackageRef,
				Version:    resolvedID.Version,
				Scope:      provider.Id(),
			}
			slots[index] = slot{ok: true, res: remoteData}
		}(i, provider)
	}

	wg.Wait()

	results := make([]types.ResolvedPackage, 0, len(providers))
	providerErrors := make([]ProviderError, 0)
	for _, item := range slots {
		if item.ok {
			results = append(results, item.res)
		}
		if item.failed {
			providerErrors = append(providerErrors, item.err)
		}
	}

	return results, providerErrors
}

// GetInfoHedged executes info on all providers in parallel and returns the
// first successful result. Ref.Scope holds the provider that answered.
func GetInfoHedged(
	providers []upstream.InfoSource,
	ref types.PackageRef,
) (upstream.Info, []ProviderError, error) {
	if len(providers) == 0 {
		return upstream.Info{}, nil, ErrNoProviderSucceeded
	}

	resChan := make(chan upstream.Info, len(providers))
	errChan := make(chan ProviderError, len(providers))

	for _, provider := range providers {
		go func(provider upstream.InfoSource) {
			metadata, err := provider.Info(ref)
			if err != nil {
				errChan <- ProviderError{
					Source: provider.Id(),
					Err:    fmt.Errorf("information failed: %w", err),
				}
				return
			}
			resChan <- upstream.Info{
				Ref: types.ScopedPackageRef{
					PackageRef: ref,
					Scope:      provider.Id(),
				},
				Metadata: metadata,
			}
		}(provider)
	}

	providerErrors := make([]ProviderError, 0, len(providers))
	pending := len(providers)

	for pending > 0 {
		select {
		case result := <-resChan:
			return result, providerErrors, nil
		case providerErr := <-errChan:
			providerErrors = append(providerErrors, providerErr)
			pending--
		}
	}

	return upstream.Info{}, providerErrors, joinProviderErrors(providerErrors)
}

// ArtifactLookup is the result of a hash lookup on the upstream mappers.
// Ref is zero when no mapper matched the artifact. Errors holds the failure
// of each mapper. A lookup with errors is incomplete, not a miss.
type ArtifactLookup struct {
	Ref    types.FullPackageRef
	Errors []ProviderError
}

// Matched reports whether some mapper identified the artifact.
func (l ArtifactLookup) Matched() bool { return l.Ref.Name != "" }

// Complete reports whether every attempted mapper finished without failure.
func (l ArtifactLookup) Complete() bool { return len(l.Errors) == 0 }

// ResolveArtifactByHash resolves an artifact's canonical upstream reference.
// Mappers run in policy order; a failed or missing match does not stop later
// mappers. Provider failures are returned for callers that surface warnings.
func ResolveArtifactByHash(artifact upstream.Hashable) ArtifactLookup {
	result := ArtifactLookup{}
	for _, mapper := range artifactMappers() {
		ref, _, ok, err := mapper.PackageByHash(artifact)
		if err != nil {
			result.Errors = append(result.Errors, ProviderError{
				Source: mapper.Id(),
				Err:    err,
			})
			continue
		}
		if ok && ref.Name != "" {
			result.Ref = ref
			return result
		}
	}
	return result
}

// DependenciesMany executes Dependencies on all providers in parallel and
// returns all successful results. An error is returned only when every provider
// fails; partial failures are collected in the returned []ProviderError slice.
func DependenciesMany(
	providers []upstream.PackageSource,
	id types.VersionedPackageRef,
) ([]types.PackageDependencies, []ProviderError) {
	if len(providers) == 0 {
		return nil, nil
	}

	type slot struct {
		res    types.PackageDependencies
		err    ProviderError
		ok     bool
		failed bool
	}

	slots := make([]slot, len(providers))
	var wg sync.WaitGroup

	for i, provider := range providers {
		wg.Add(1)
		go func(index int, provider upstream.PackageSource) {
			defer wg.Done()
			deps, err := provider.Dependencies(id)
			if err != nil {
				slots[index] = slot{
					failed: true,
					err: ProviderError{
						Source: provider.Id(),
						Err:    err,
					},
				}
				return
			}
			result := types.PackageDependencies{}
			if deps != nil {
				result = *deps
			}
			slots[index] = slot{ok: true, res: result}
		}(i, provider)
	}

	wg.Wait()

	results := make([]types.PackageDependencies, 0, len(providers))
	providerErrors := make([]ProviderError, 0)
	for _, item := range slots {
		if item.ok {
			results = append(results, item.res)
		}
		if item.failed {
			providerErrors = append(providerErrors, item.err)
		}
	}

	if len(results) == 0 && len(providerErrors) > 0 {
		return nil, providerErrors
	}

	return results, providerErrors
}

func joinProviderErrors(providerErrors []ProviderError) error {
	if len(providerErrors) == 0 {
		return ErrNoProviderSucceeded
	}
	joined := make([]error, 0, len(providerErrors))
	for _, providerErr := range providerErrors {
		joined = append(joined, providerErr)
	}
	return errors.Join(joined...)
}
