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
	Source types.Source
	Err    error
}

func (e ProviderError) Error() string {
	return fmt.Sprintf("%s: %v", e.Source.String(), e.Err)
}

func (e ProviderError) Unwrap() error {
	return e.Err
}

type InfoResult struct {
	Information types.ProjectInformation
	Fetch       upstream.FetchResult
}

// SearchMany executes search on all providers in parallel.
//
// Default behavior is non-aggregated: each provider contributes one
// types.SearchResults item in the returned slice.
func SearchMany(
	providers []upstream.Provider,
	query types.ProjectName,
	options types.SearchOptions,
) ([]types.SearchResults, []ProviderError) {
	if len(providers) == 0 {
		return nil, nil
	}

	type slot struct {
		res    types.SearchResults
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
			res, err := upstream.Search(provider, query, options)
			if err != nil {
				slots[index] = slot{
					failed: true,
					err: ProviderError{
						Source: provider.Source(),
						Err:    err,
					},
				}
				return
			}
			slots[index] = slot{ok: true, res: res}
		}(i, provider)
	}

	wg.Wait()

	results := make([]types.SearchResults, 0, len(providers))
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
	id types.PackageId,
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
			remoteData, err := upstream.Fetch(provider, id)
			if err != nil {
				slots[index] = slot{
					failed: true,
					err: ProviderError{
						Source: provider.Source(),
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

// FirstFetch executes fetch on all providers in parallel and returns the first
// successful result.
func FirstFetch(
	providers []upstream.Provider,
	id types.PackageId,
) (upstream.FetchResult, []ProviderError, error) {
	// TODO: implement this function in a way that doesn't wait for all providers
	//  to finish if one has already succeeded
	panic("not implemented")
}

// FirstInfo executes info+fetch on all providers in parallel and returns the
// first successful result.
func FirstInfo(
	providers []upstream.Provider,
	id types.PackageId,
) (InfoResult, []ProviderError, error) {
	if len(providers) == 0 {
		return InfoResult{}, nil, ErrNoProviderSucceeded
	}

	results := make(chan InfoResult, len(providers))
	errorsChan := make(chan ProviderError, len(providers))

	for _, provider := range providers {
		go func(provider upstream.Provider) {
			info, err := upstream.Information(provider, id.Name)
			if err != nil {
				errorsChan <- ProviderError{
					Source: provider.Source(),
					Err:    fmt.Errorf("information failed: %w", err),
				}
				return
			}

			remoteData, err := upstream.Fetch(provider, id)
			if err != nil {
				errorsChan <- ProviderError{
					Source: provider.Source(),
					Err:    fmt.Errorf("fetch failed: %w", err),
				}
				return
			}

			results <- InfoResult{Information: info, Fetch: remoteData}
		}(provider)
	}

	providerErrors := make([]ProviderError, 0, len(providers))
	pending := len(providers)

	for pending > 0 {
		select {
		case result := <-results:
			return result, providerErrors, nil
		case providerErr := <-errorsChan:
			providerErrors = append(providerErrors, providerErr)
			pending--
		}
	}

	return InfoResult{}, providerErrors, joinProviderErrors(providerErrors)
}

// DependenciesMany executes Dependencies on all providers in parallel and
// returns all successful results. An error is returned only when every provider
// fails; partial failures are collected in the returned []ProviderError slice.
func DependenciesMany(
	providers []upstream.Provider,
	id types.PackageId,
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
						Source: provider.Source(),
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
