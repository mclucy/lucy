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

type InfoResult struct {
	Information types.Metadata
	Fetch       upstream.FetchResult
}

// SearchMany executes search on all providers in parallel.
//
// Default behavior is non-aggregated: each provider contributes one
// upstream.SearchResponse item in the returned slice.
func SearchMany(
	providers []SearchProvider,
	query types.BarePackageName,
	options types.SearchOptions,
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
		wg.Add(1)
		go func(index int, provider SearchProvider) {
			defer wg.Done()
			res, err := upstream.Search(provider.Searcher, upstream.Query{
				Keyword:        query.String(),
				SortBy:         options.SortBy,
				ExcludeClient:  !options.IncludeClient,
				FilterPlatform: options.FilterPlatform,
			})
			if err != nil {
				slots[index] = slot{
					failed: true,
					err: ProviderError{
						Source: provider.Source,
						Err:    err,
					},
				}
				return
			}
			if res.Source == types.SourceUnknown {
				res.Source = provider.Source
			}
			slots[index] = slot{ok: true, res: res}
		}(i, provider)
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
	providers []upstream.Provider,
	id types.VersionedPackageRef,
) ([]upstream.FetchResult, []ProviderError) {
	if len(providers) == 0 {
		return nil, nil
	}

	type slot struct {
		res    upstream.FetchResult
		err    ProviderError
		ok     bool
		failed bool
	}

	slots := make([]slot, len(providers))
	var wg sync.WaitGroup

	for i, provider := range providers {
		wg.Add(1)
		go func(index int, provider upstream.Provider) {
			defer wg.Done()
			resolver, ok, err := GetVersionSelectorResolver(provider.Id())
			if err == nil && !ok {
				err = fmt.Errorf("%w: %s", ErrUnsupportedSource, provider.Id())
			}
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

			remoteData, err := upstream.Fetch(provider, resolver.Resolver, id)
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
			slots[index] = slot{ok: true, res: remoteData}
		}(i, provider)
	}

	wg.Wait()

	results := make([]upstream.FetchResult, 0, len(providers))
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
// first successful result.
func GetInfoHedged(
	providers []InfoProvider,
	ref types.PackageRef,
) (types.Metadata, []ProviderError, error) {
	if len(providers) == 0 {
		return types.Metadata{}, nil, ErrNoProviderSucceeded
	}

	resChan := make(chan types.Metadata, len(providers))
	errChan := make(chan ProviderError, len(providers))

	for _, provider := range providers {
		go func(provider InfoProvider) {
			res, err := upstream.Info(provider.Informer, ref)
			if err != nil {
				errChan <- ProviderError{
					Source: provider.Source,
					Err:    fmt.Errorf("information failed: %w", err),
				}
				return
			}
			resChan <- res
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

	return types.Metadata{}, providerErrors, joinProviderErrors(providerErrors)
}

// DependenciesMany executes Dependencies on all providers in parallel and
// returns all successful results. An error is returned only when every provider
// fails; partial failures are collected in the returned []ProviderError slice.
func DependenciesMany(
	providers []upstream.Provider,
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
		go func(index int, provider upstream.Provider) {
			defer wg.Done()
			deps, err := upstream.Dependencies(provider, id)
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
