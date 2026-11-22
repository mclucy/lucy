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

// SearchMany executes search on all providers in parallel.
//
// Each provider contributes one upstream.SearchResponse item in the returned
// slice, kept separate from the other providers' results.
func (r *Router) SearchMany(
	ctx context.Context,
	providers []upstream.SearchProvider,
	query upstream.Query,
) ([]upstream.SearchResponse, []upstream.ProviderError) {
	if len(providers) == 0 {
		return nil, nil
	}

	type slot struct {
		res    upstream.SearchResponse
		err    upstream.ProviderError
		ok     bool
		failed bool
	}

	slots := make([]slot, len(providers))
	var wg sync.WaitGroup

	for i, provider := range providers {
		wg.Go(func() {
			res, err := provider.Search(ctx, query)
			if err == nil && len(res.Items) == 0 {
				err = fmt.Errorf(
					"no projects found for \"%s\"",
					query.Keyword,
				)
			}
			if err != nil {
				slots[i] = slot{
					failed: true,
					err: upstream.ProviderError{
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
	providerErrors := make([]upstream.ProviderError, 0)
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
) (upstream.Info, []upstream.ProviderError, error) {
	if len(providers) == 0 {
		return upstream.Info{}, nil, ErrNoProviderSucceeded
	}
	if err := ctx.Err(); err != nil {
		return upstream.Info{}, nil, err
	}

	resChan := make(chan upstream.Info, len(providers))
	errChan := make(chan upstream.ProviderError, len(providers))

	for _, provider := range providers {
		go func(provider upstream.InfoProvider) {
			metadata, err := provider.Info(ctx, ref)
			if err != nil {
				errChan <- upstream.ProviderError{Provider: provider.Provider(), Err: fmt.Errorf("information failed: %w", err)}
				return
			}
			resChan <- upstream.Info{
				Ref:      catalog.Ref{Name: ref.Name, Provider: provider.Provider()},
				Metadata: metadata,
			}
		}(provider)
	}

	providerErrors := make([]upstream.ProviderError, 0, len(providers))
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

// ResolveArtifactByHash resolves an artifact's canonical upstream release.
// Mappers run in policy order; a failed or missing match does not stop later
// mappers. Provider failures are returned for callers that surface warnings.
func (r *Router) ResolveArtifactByHash(
	ctx context.Context,
	artifact upstream.ArtifactFingerprint,
) upstream.ArtifactReleaseResolution {
	resolutions := upstream.LookupArtifactReleases(
		ctx,
		[]upstream.ArtifactFingerprint{artifact},
		r.artifactReleaseMappers(),
		namedRelease,
	)
	return resolutions[0]
}

// ResolveArtifactsByHash resolves canonical upstream releases for artifacts
// in input order. Only a named release is reported; provider failures stay
// attached to the artifact they affected, including failures from mappers
// that ran before a later one matched.
func (r *Router) ResolveArtifactsByHash(
	ctx context.Context,
	artifacts []upstream.ArtifactFingerprint,
) []upstream.ArtifactReleaseResolution {
	return upstream.LookupArtifactReleases(
		ctx,
		artifacts,
		r.artifactReleaseMappers(),
		namedRelease,
	)
}

// namedRelease accepts the release a mapper matched only when it names its
// project, which is the identity resolution and adoption can use.
func namedRelease(release upstream.Release) bool {
	return release.Ref.Name != ""
}

func joinProviderErrors(providerErrors []upstream.ProviderError) error {
	if len(providerErrors) == 0 {
		return ErrNoProviderSucceeded
	}
	joined := make([]error, 0, len(providerErrors))
	for _, providerErr := range providerErrors {
		joined = append(joined, providerErr)
	}
	return errors.Join(joined...)
}
