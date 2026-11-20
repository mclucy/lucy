package upstream

import (
	"context"
	"fmt"

	"github.com/mclucy/lucy/catalog"
)

// ProviderError pairs a provider identity with the failure it reported.
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

// ArtifactReleaseResolution is the outcome of identifying one fingerprint on
// the upstream mappers: the full matched release and its provider, or the
// failures that kept the artifact unclaimed. A resolution with errors is
// incomplete, not a miss. The release lets a caller that already paid for the
// lookup reuse its identity instead of asking the provider chain again.
//
// Release and Provider are set only when a mapper reported a match and the
// accept filter, when present, allowed it. Errors holds the failures reported
// for this fingerprint, including failures from mappers that ran before a
// later one matched it.
type ArtifactReleaseResolution struct {
	Release  Release
	Matched  bool
	Provider catalog.Provider
	Errors   []ProviderError
}

// Complete reports whether every attempted mapper finished without failure.
func (r ArtifactReleaseResolution) Complete() bool { return len(r.Errors) == 0 }

// LookupArtifactReleases resolves fingerprints to published releases in input
// order. It returns one resolution per input, or nil when there are none.
//
// Mappers run in policy order. After each mapper only the fingerprints it did
// not resolve continue to the next mapper, so a provider is asked about an
// artifact only while no earlier provider has identified it. A mapper failure
// is recorded against just the fingerprints that mapper was asked about, and
// resolution continues with later mappers.
//
// Mappers that implement ArtifactBatchReleaseMapper receive all pending
// fingerprints in one call. Other mappers are asked one fingerprint at a time,
// and a lone pending fingerprint also uses the single-fingerprint path.
//
// A mapper-reported match resolves the fingerprint. When accept is non-nil the
// release also has to satisfy it; a rejected match leaves the fingerprint
// unresolved for later mappers.
func LookupArtifactReleases(
	ctx context.Context,
	fingerprints []ArtifactFingerprint,
	mappers []ArtifactReleaseMapper,
	accept func(Release) bool,
) []ArtifactReleaseResolution {
	if len(fingerprints) == 0 {
		return nil
	}

	resolutions := make([]ArtifactReleaseResolution, len(fingerprints))
	pending := len(fingerprints)
	var scratch []ArtifactFingerprint

	for _, mapper := range mappers {
		if pending == 0 {
			break
		}
		provider := mapper.Provider()
		artifacts := fingerprints
		if pending != len(fingerprints) {
			if scratch == nil {
				scratch = make([]ArtifactFingerprint, 0, len(fingerprints))
			}
			scratch = scratch[:0]
			for i := range resolutions {
				if !resolutions[i].Matched {
					scratch = append(scratch, fingerprints[i])
				}
			}
			artifacts = scratch
		}

		lookups := mapperReleasesByHash(ctx, mapper, artifacts)
		next := 0
		for i := range resolutions {
			if resolutions[i].Matched {
				continue
			}
			result := lookups[next]
			next++
			if result.Err != nil {
				resolutions[i].Errors = append(
					resolutions[i].Errors,
					ProviderError{Provider: provider, Err: result.Err},
				)
				continue
			}
			if !result.Matched {
				continue
			}
			if accept != nil && !accept(result.Release) {
				continue
			}
			resolutions[i].Release = result.Release
			resolutions[i].Matched = true
			resolutions[i].Provider = provider
			pending--
		}
	}
	return resolutions
}

// mapperReleasesByHash asks one mapper about artifacts in input order.
// Batch-capable mappers answer in a single call; the rest are asked one
// artifact at a time. A lone artifact takes the single-fingerprint path even
// for a batch-capable mapper, where the query is identical and no batching is
// possible.
func mapperReleasesByHash(
	ctx context.Context,
	mapper ArtifactReleaseMapper,
	artifacts []ArtifactFingerprint,
) []ArtifactReleaseLookup {
	if len(artifacts) == 1 {
		release, matched, err := mapper.ReleaseByHash(ctx, artifacts[0])
		return []ArtifactReleaseLookup{{
			Release: release,
			Matched: matched,
			Err:     err,
		}}
	}
	if batch, ok := mapper.(ArtifactBatchReleaseMapper); ok {
		return batch.ReleasesByHash(ctx, artifacts)
	}
	lookups := make([]ArtifactReleaseLookup, len(artifacts))
	for i, artifact := range artifacts {
		lookups[i].Release,
			lookups[i].Matched,
			lookups[i].Err = mapper.ReleaseByHash(ctx, artifact)
	}
	return lookups
}
