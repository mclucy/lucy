package telemetry

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
)

const (
	maxResponseBytes = 1 << 20

	maxUploadCounter = 10_000
	// maxUploadQueueMS bounds queue_ms at the seven-day record retention.
	maxUploadQueueMS = int64(7 * 24 * time.Hour / time.Millisecond)
	// maxUploadEnvelopeBytes is a conservative upper bound for the serialized
	// `,"upload":{"queue_ms":…,"attempts":…,"throttles":…}` member.
	maxUploadEnvelopeBytes = 96
	intakeLimitedReason    = "intake_limited"
)

type wireRecord struct {
	ID      string          `json:"id"`
	Type    recordType      `json:"type"`
	Payload jsontext.Value  `json:"payload"`
	Upload  *uploadEnvelope `json:"upload,omitempty"`
}

// uploadEnvelope is transport metadata injected at POST time. It is excluded
// from a record's immutable digest and never alters the queued payload.
type uploadEnvelope struct {
	QueueMS   int64 `json:"queue_ms"`
	Attempts  int   `json:"attempts"`
	Throttles int   `json:"throttles"`
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
	throttled    bool
	throttledIDs []string
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

func (c *Client) uploadRecords(ctx context.Context, prepared []preparedRecord, state *persistedState) (uploadResult, error) {
	entries := make(map[string]*queueEntry, len(prepared))
	for index := range state.Queue {
		entries[state.Queue[index].ID] = &state.Queue[index]
	}
	var lastErr error
	for attempt := 1; attempt <= c.limits.MaxAttempts; attempt++ {
		records := c.stampUploads(prepared, entries, c.now())
		outcome, err := c.postOnce(ctx, records)
		if outcome.attempted {
			c.recordAttempt(state, outcome)
			recordUploadAttempts(prepared, entries)
		}
		if err != nil {
			if failure, isHTTP := errors.AsType[*httpError](err); isHTTP && failure.throttled {
				recordUploadThrottles(state, prepared, entries, failure.throttledIDs)
			}
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

// stampUploads copies the queued wire records and attaches event-only
// transport metadata measured at this attempt. Reports keep their legacy
// record shape and size. A legacy event with no room for the envelope is sent
// without it rather than being rejected or altered.
func (c *Client) stampUploads(prepared []preparedRecord, entries map[string]*queueEntry, now time.Time) []wireRecord {
	records := make([]wireRecord, len(prepared))
	for index, item := range prepared {
		wire := item.wire
		if item.entry.Type == recordTypeEvent && item.wireBytes+maxUploadEnvelopeBytes <= c.limits.MaxRecordBytes {
			attempts := 1
			throttles := 0
			if entry := entries[item.entry.ID]; entry != nil {
				attempts = min(entry.UploadAttempts+1, maxUploadCounter)
				throttles = min(max(entry.UploadThrottles, 0), maxUploadCounter)
			}
			wire.Upload = &uploadEnvelope{
				QueueMS:   uploadQueueMS(item.entry.CreatedAt, now),
				Attempts:  attempts,
				Throttles: throttles,
			}
		}
		records[index] = wire
	}
	return records
}

// uploadQueueMS measures time spent durably queued. Skewed clocks clamp to
// zero so the envelope stays inside its declared bounds.
func uploadQueueMS(createdAt time.Time, now time.Time) int64 {
	if createdAt.IsZero() {
		return 0
	}
	elapsed := now.Sub(createdAt).Milliseconds()
	return min(max(elapsed, 0), maxUploadQueueMS)
}

func recordUploadAttempts(prepared []preparedRecord, entries map[string]*queueEntry) {
	for _, item := range prepared {
		if item.entry.Type != recordTypeEvent {
			continue
		}
		entry := entries[item.entry.ID]
		if entry == nil {
			continue
		}
		entry.UploadAttempts = min(entry.UploadAttempts+1, maxUploadCounter)
	}
}

// recordUploadThrottles records one intake-limited delivery attempt. A
// retryable response that names limited records bumps only those; a batch-level
// response bumps every event in the attempt. Nothing is removed from the queue.
func recordUploadThrottles(state *persistedState, prepared []preparedRecord, entries map[string]*queueEntry, ids []string) {
	if state.Transport.Throttles < math.MaxInt64 {
		state.Transport.Throttles++
	}
	limited := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		limited[id] = struct{}{}
	}
	for _, item := range prepared {
		if item.entry.Type != recordTypeEvent {
			continue
		}
		if len(limited) > 0 {
			if _, ok := limited[item.entry.ID]; !ok {
				continue
			}
		}
		entry := entries[item.entry.ID]
		if entry == nil {
			continue
		}
		entry.UploadThrottles = min(entry.UploadThrottles+1, maxUploadCounter)
	}
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
		failure := &httpError{detail: "read response: " + sanitizeErrorText(readErr), retryable: true}
		if response.StatusCode == http.StatusTooManyRequests {
			failure.throttled = true
		}
		return outcome, failure
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
		if failure.retryable {
			failure.throttled = response.StatusCode == http.StatusTooManyRequests
			noteIntakeLimited(failure, responseBody, records)
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

// noteIntakeLimited recognizes only the known intake_limited rejection reason
// on a retryable response. It never marks a record as permanently rejected and
// never removes anything from the queue.
func noteIntakeLimited(failure *httpError, body []byte, records []wireRecord) {
	var decoded wireResponse
	if err := json.Unmarshal(body, &decoded); err != nil || len(decoded.Rejected) == 0 {
		return
	}
	known := make(map[string]struct{}, len(records))
	for _, record := range records {
		known[record.ID] = struct{}{}
	}
	seen := make(map[string]struct{}, len(decoded.Rejected))
	for _, rejection := range decoded.Rejected {
		if strings.TrimSpace(rejection.Reason) != intakeLimitedReason {
			continue
		}
		failure.throttled = true
		id, err := normalizeUUID(rejection.ID)
		if err != nil {
			continue
		}
		if _, ok := known[id]; !ok {
			continue
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		failure.throttledIDs = append(failure.throttledIDs, id)
	}
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
