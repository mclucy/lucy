package telemetry

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const maxResponseBytes = 1 << 20

type wireRecord struct {
	ID      string         `json:"id"`
	Type    recordType     `json:"type"`
	Payload jsontext.Value `json:"payload"`
}

type wireRequest struct {
	Records []wireRecord `json:"records"`
}

type wireRejection struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

type wireResponse struct {
	Accepted *[]string       `json:"accepted"`
	Rejected []wireRejection `json:"rejected"`
}

type attemptKind int

const (
	attemptSucceeded attemptKind = iota + 1
	attemptRejected
	attemptUncertain
)

type attemptOutcome struct {
	kind         attemptKind
	attempted    bool
	accepted     []string
	rejected     []wireRejection
	requestBytes int64
}

type httpError struct {
	status       int
	detail       string
	retryable    bool
	batchInvalid bool
}

func (failure *httpError) Error() string {
	if failure.status > 0 {
		return fmt.Sprintf("telemetry: server returned %d: %s", failure.status, failure.detail)
	}
	return "telemetry: request failed: " + failure.detail
}

type uploadResult struct {
	accepted     []string
	rejected     []wireRejection
	batchInvalid bool
}

func (c *Client) checkHealth(ctx context.Context) error {
	requestContext, cancel := context.WithTimeout(ctx, c.limits.HealthTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestContext, http.MethodGet, c.endpoint+"/health", nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", c.userAgent())
	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("telemetry: health check returned %s", response.Status)
	}
	return nil
}

func (c *Client) uploadRecords(ctx context.Context, records []wireRecord, state *persistedState) (uploadResult, error) {
	var lastErr error
	for attempt := 1; attempt <= c.limits.MaxAttempts; attempt++ {
		outcome, err := c.postOnce(ctx, records)
		if outcome.attempted {
			c.recordAttempt(state, outcome)
		}
		if err == nil {
			return uploadResult{
				accepted: outcome.accepted,
				rejected: outcome.rejected,
			}, nil
		}
		lastErr = err
		failure, isHTTP := errors.AsType[*httpError](err)
		retryable := isHTTP && failure.retryable
		if !retryable || attempt == c.limits.MaxAttempts || ctx.Err() != nil {
			if isHTTP {
				return uploadResult{batchInvalid: failure.batchInvalid}, err
			}
			return uploadResult{}, err
		}
		delay := c.limits.RetryBackoff
		for step := 1; step < attempt; step++ {
			delay = min(delay*2, 2*time.Second)
		}
		if err := sleepContext(ctx, delay); err != nil {
			return uploadResult{}, err
		}
	}
	return uploadResult{}, lastErr
}

func (c *Client) recordAttempt(state *persistedState, outcome attemptOutcome) {
	state.Transport.Requests++
	state.Transport.RequestBodyBytes += outcome.requestBytes
	state.Transport.LastRequestBodyBytes = outcome.requestBytes
	switch outcome.kind {
	case attemptSucceeded:
		state.Transport.Succeeded++
	case attemptRejected:
		state.Transport.RejectedRequests++
	case attemptUncertain:
		state.Transport.Uncertain++
	}
}

func (c *Client) postOnce(ctx context.Context, records []wireRecord) (attemptOutcome, error) {
	body, err := marshalCanonical(wireRequest{Records: records})
	if err != nil {
		return attemptOutcome{}, fmt.Errorf("telemetry: encode request: %w", err)
	}
	if len(body) > c.limits.MaxRequestBytes {
		return attemptOutcome{}, fmt.Errorf("telemetry: request body is %d bytes, limit %d", len(body), c.limits.MaxRequestBytes)
	}
	requestContext, cancel := context.WithTimeout(ctx, c.limits.RequestTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestContext, http.MethodPost, c.endpoint+"/v1/records", bytes.NewReader(body))
	if err != nil {
		return attemptOutcome{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", c.userAgent())
	outcome := attemptOutcome{attempted: true, requestBytes: int64(len(body))}
	response, err := c.http.Do(request)
	if err != nil {
		return attemptOutcome{kind: attemptUncertain, attempted: true, requestBytes: int64(len(body))}, &httpError{
			detail:    sanitizeErrorText(err),
			retryable: true,
		}
	}
	defer response.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if readErr != nil {
		outcome.kind = attemptUncertain
		return outcome, &httpError{detail: "read response: " + sanitizeErrorText(readErr), retryable: true}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		detail, _ := sanitizeText(string(responseBody), 256)
		detail = strings.Join(strings.Fields(detail), " ")
		if detail == "" {
			detail = response.Status
		}
		failure := &httpError{
			status:       response.StatusCode,
			detail:       detail,
			retryable:    retryableStatus(response.StatusCode),
			batchInvalid: response.StatusCode == http.StatusBadRequest,
		}
		outcome.kind = attemptRejected
		return outcome, failure
	}
	if len(responseBody) > maxResponseBytes {
		outcome.kind = attemptUncertain
		return outcome, &httpError{detail: "response exceeds size limit", retryable: true}
	}
	var decoded wireResponse
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		outcome.kind = attemptUncertain
		return outcome, &httpError{detail: "invalid response: " + sanitizeErrorText(err), retryable: true}
	}
	if decoded.Accepted == nil {
		outcome.kind = attemptUncertain
		return outcome, &httpError{detail: "response omitted accepted ids", retryable: true}
	}
	accepted := make([]string, 0, len(*decoded.Accepted))
	for _, raw := range *decoded.Accepted {
		id, err := normalizeUUID(raw)
		if err != nil {
			outcome.kind = attemptUncertain
			return outcome, &httpError{detail: "response contained an invalid accepted id", retryable: true}
		}
		accepted = append(accepted, id)
	}
	rejected := make([]wireRejection, 0, len(decoded.Rejected))
	for _, raw := range decoded.Rejected {
		id, err := normalizeUUID(raw.ID)
		if err != nil {
			outcome.kind = attemptUncertain
			return outcome, &httpError{detail: "response contained an invalid rejected id", retryable: true}
		}
		reason, _ := sanitizeText(raw.Reason, 128)
		rejected = append(rejected, wireRejection{ID: id, Reason: strings.Join(strings.Fields(reason), " ")})
	}
	outcome.kind = attemptSucceeded
	outcome.accepted = accepted
	outcome.rejected = rejected
	return outcome, nil
}

func retryableStatus(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusTooEarly, http.StatusTooManyRequests:
		return true
	}
	return status >= 500
}

func (c *Client) userAgent() string {
	if c.version == "" {
		return "lucy-telemetry"
	}
	return "lucy-telemetry/" + c.version
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
