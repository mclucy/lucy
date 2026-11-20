package curseforge

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/upstream/internal/providers/retry"
)

// Docs: https://docs.curseforge.com/rest-api/
const baseUrl = "https://api.curseforge.com"

// maxLookupResponseBytes bounds one batched lookup response. CurseForge
// documents no cap on request arrays; lookupBatchSize bounds them here, so a
// response stays bounded even when a project carries many files.
const maxLookupResponseBytes = 16 << 20

// get performs an authenticated GET request to the CurseForge API and
// unmarshals the JSON response into dest.
func (p *Provider) get(ctx context.Context, url string, dest any) error {
	if p.apiKey == "" {
		return ErrNoApiKey
	}

	p.logger.Debug("curseforge api: GET " + url)

	headers := http.Header{}
	headers.Set("x-api-key", p.apiKey)
	headers.Set("Accept", "application/json")
	res, err := p.network.CachedGetRequest(
		ctx,
		url,
		cache.BytesRequestOptions{
			Kind:    cache.KindMetadata,
			Headers: headers,
		},
	)
	if attempt := lookupAttempt(res, err); attempt.Status != http.StatusOK {
		return lookupOutcome(attempt)
	}
	if err != nil {
		return fmt.Errorf("curseforge: request failed: %w", err)
	}

	if err := json.Unmarshal(res.Data, dest); err != nil {
		return fmt.Errorf("curseforge: failed to parse response: %w", err)
	}

	return nil
}

// readPost performs an authenticated read-only JSON POST lookup to the
// CurseForge API and unmarshals a 200 response into dest. Successful responses
// are cached by URL, body, and credential. Transient statuses are retried with
// bounded backoff. A final non-200 status is returned as ApiResponseError so
// callers can tell a missing resource (404) from a failed lookup.
//
// Only lookups may use it. A request that mutates remote state must go through
// the transport directly so a repeated attempt cannot repeat the mutation.
func (p *Provider) readPost(
	ctx context.Context,
	url string,
	payload, dest any,
) error {
	if p.apiKey == "" {
		return retry.Permanent(ErrNoApiKey)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return retry.Permanent(
			fmt.Errorf("curseforge: failed to encode request: %w", err),
		)
	}

	headers := http.Header{}
	headers.Set("x-api-key", p.apiKey)
	headers.Set("Content-Type", "application/json")
	headers.Set("Accept", "application/json")

	p.logger.Debug("curseforge api: POST " + url)

	outcome := retry.Do(ctx, retry.Default, func(ctx context.Context) retry.Attempt {
		res, err := p.network.CachedPostRequest(
			ctx,
			url,
			body,
			cache.BytesRequestOptions{
				Kind:     cache.KindMetadata,
				Headers:  headers,
				MaxBytes: maxLookupResponseBytes,
			},
			func(raw []byte) error {
				if err := json.Unmarshal(raw, dest); err != nil {
					return retry.Permanent(fmt.Errorf("curseforge: failed to parse response: %w", err))
				}
				return nil
			},
		)
		if res != nil && res.StatusCode != http.StatusOK {
			p.logger.Debug(
				fmt.Sprintf(
					"curseforge api: POST %s returned status %d",
					url,
					res.StatusCode,
				),
			)
		}
		return lookupAttempt(res, err)
	})
	return lookupOutcome(outcome)
}

// lookupAttempt retains the response status and retry headers even when
// reading a non-2xx body failed, so the status still classifies the failure.
func lookupAttempt(res *cache.BytesResponse, err error) retry.Attempt {
	attempt := retry.Attempt{Err: err}
	if res != nil {
		attempt.Status = res.StatusCode
		attempt.RetryAfter = retry.RetryAfter(res.Header)
	}
	return attempt
}

// lookupOutcome converts a finished lookup. A non-200 status that asked for a
// delay becomes a ThrottledError so no smaller batch or later request is sent
// before it passes. A final non-200 status keeps its meaning, so a 404 still
// reads as a missing resource rather than a read failure, and any body read
// failure stays in the error chain as diagnostics.
func lookupOutcome(outcome retry.Attempt) error {
	if outcome.Status == 0 {
		if outcome.Err == nil {
			return fmt.Errorf("curseforge: lookup returned no response")
		}
		return fmt.Errorf("curseforge: request failed: %w", outcome.Err)
	}
	if outcome.Status != http.StatusOK {
		var statusErr error
		if retry.TransientStatus(outcome.Status) && outcome.RetryAfter > 0 {
			statusErr = &retry.ThrottledError{
				Status:     outcome.Status,
				RetryAfter: outcome.RetryAfter,
			}
		} else {
			statusErr = ErrApiResponse(outcome.Status)
		}
		if outcome.Err != nil {
			return errors.Join(statusErr, outcome.Err)
		}
		return statusErr
	}
	if outcome.Err != nil {
		return fmt.Errorf("curseforge: request failed: %w", outcome.Err)
	}
	return nil
}

// isNotFound reports whether err is an API 404 response.
func isNotFound(err error) bool {
	var apiErr ApiResponseError
	return errors.As(err, &apiErr) &&
		apiErr.StatusCode == http.StatusNotFound
}
