package routing

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/upstream"
)

var ErrNoProviderSucceeded = errors.New("no provider succeeded")

type ProviderError struct {
	// Provider identifies the semantic upstream label for user-facing diagnostics.
	// The failed runtime executor is a Provider implementation.
	Provider catalog.Provider
	Err      error
}

func (e ProviderError) Error() string {
	return fmt.Sprintf("%s: %v", e.Provider.String(), e.Err)
}

func (e ProviderError) Unwrap() error {
	return e.Err
}

// SearchMany executes search on all providers in parallel.
//
// Default behavior is non-aggregated: each provider contributes one
// upstream.SearchResponse item in the returned slice.
func (r *Router) SearchMany(
	ctx context.Context,
	providers []upstream.SearchProvider,
	query upstream.Query,
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
			res, err := upstream.Search(ctx, provider, query)
			if err != nil {
				slots[i] = slot{
					failed: true,
					err: ProviderError{
						Provider: provider.Provider(),
						Err:      err,
					},
				}
				return
			}
			if res.Provider == catalog.ProviderUnknown {
				res.Provider = provider.Provider()
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
func (r *Router) FetchMany(
	ctx context.Context,
	local upstream.LocalContext,
	providers []upstream.PackageProvider,
	id catalog.VersionedRef,
) ([]upstream.ResolvedPackage, []ProviderError) {
	if len(providers) == 0 {
		return nil, nil
	}

	type slot struct {
		res    upstream.ResolvedPackage
		err    ProviderError
		ok     bool
		failed bool
	}

	slots := make([]slot, len(providers))
	var wg sync.WaitGroup

	for i, provider := range providers {
		wg.Go(func() {
			resolvedID, err := provider.ResolveVersionSelector(ctx, local, id)
			if err != nil {
				slots[i] = slot{
					failed: true,
					err: ProviderError{
						Provider: provider.Provider(),
						Err:      err,
					},
				}
				return
			}

			remoteData, err := provider.Fetch(ctx, local, resolvedID)
			if err != nil {
				slots[i] = slot{
					failed: true,
					err: ProviderError{
						Provider: provider.Provider(),
						Err:      err,
					},
				}
				return
			}
			// Routing binds the provider's concrete identity to the selected
			// ecosystem/name/version coordinate returned by resolution.
			remoteData.ID = catalog.VersionedRef{
				Name:      resolvedID.Name,
				Provider:  provider.Provider(),
				Ecosystem: resolvedID.Ecosystem,
				Version:   resolvedID.Version,
			}
			slots[i] = slot{ok: true, res: remoteData}
		})
	}

	wg.Wait()

	results := make([]upstream.ResolvedPackage, 0, len(providers))
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

// GetInfoFirstSuccess executes info on all providers in parallel and returns the
// first successful result. Ref.Provider records the provider that answered.
func (r *Router) GetInfoFirstSuccess(
	ctx context.Context,
	providers []upstream.InfoProvider,
	ref catalog.Ref,
) (upstream.Info, []ProviderError, error) {
	if len(providers) == 0 {
		return upstream.Info{}, nil, ErrNoProviderSucceeded
	}
	if err := ctx.Err(); err != nil {
		return upstream.Info{}, nil, err
	}

	resChan := make(chan upstream.Info, len(providers))
	errChan := make(chan ProviderError, len(providers))

	for _, provider := range providers {
		go func(provider upstream.InfoProvider) {
			metadata, err := provider.Info(ctx, ref)
			if err != nil {
				errChan <- ProviderError{Provider: provider.Provider(), Err: fmt.Errorf("information failed: %w", err)}
				return
			}
			resChan <- upstream.Info{
				Ref:      catalog.Ref{Name: ref.Name, Provider: provider.Provider()},
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
		case <-ctx.Done():
			return upstream.Info{}, providerErrors, ctx.Err()
		}
	}

	return upstream.Info{}, providerErrors, joinProviderErrors(providerErrors)
}

// ArtifactLookup is the result of a hash lookup on the upstream mappers.
// Ref is zero when no mapper matched the artifact. Errors holds the failure
// of each mapper. A lookup with errors is incomplete, not a miss.
type ArtifactLookup struct {
	Ref    catalog.VersionedRef
	Errors []ProviderError
}

// Matched reports whether some mapper identified the artifact.
func (l ArtifactLookup) Matched() bool { return l.Ref.Name != "" }

// Complete reports whether every attempted mapper finished without failure.
func (l ArtifactLookup) Complete() bool { return len(l.Errors) == 0 }

// ResolveArtifactByHash resolves an artifact's canonical upstream reference.
// Mappers run in policy order; a failed or missing match does not stop later
// mappers. Provider failures are returned for callers that surface warnings.
func (r *Router) ResolveArtifactByHash(
	ctx context.Context,
	artifact upstream.ArtifactFingerprint,
) ArtifactLookup {
	result := ArtifactLookup{}
	for _, mapper := range r.artifactMappers() {
		ref, _, ok, err := mapper.PackageByHash(ctx, artifact)
		if err != nil {
			result.Errors = append(result.Errors, ProviderError{
				Provider: mapper.Provider(),
				Err:      err,
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
func (r *Router) DependenciesMany(
	ctx context.Context,
	local upstream.LocalContext,
	providers []upstream.PackageProvider,
	id catalog.VersionedRef,
) ([]upstream.DependencySet, []ProviderError) {
	if len(providers) == 0 {
		return nil, nil
	}

	type slot struct {
		res    upstream.DependencySet
		err    ProviderError
		ok     bool
		failed bool
	}

	slots := make([]slot, len(providers))
	var wg sync.WaitGroup

	for i, provider := range providers {
		wg.Go(func() {
			deps, err := provider.Dependencies(ctx, local, id)
			if err != nil {
				slots[i] = slot{
					failed: true,
					err: ProviderError{
						Provider: provider.Provider(),
						Err:      err,
					},
				}
				return
			}
			result := upstream.DependencySet{}
			if deps != nil {
				result = *deps
			}
			slots[i] = slot{ok: true, res: result}
		})
	}

	wg.Wait()

	results := make([]upstream.DependencySet, 0, len(providers))
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
