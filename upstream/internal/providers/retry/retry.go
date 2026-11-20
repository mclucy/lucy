// Package retry performs bounded retries with backoff for read-only provider
// lookups. It never wraps a mutation: callers use it only for requests whose
// repetition has no side effect.
package retry

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// StatusError carries the HTTP status of a failed lookup so callers can tell a
// transient failure from a permanent one.
type StatusError struct {
	Status int
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("status %d", e.Status)
}

// HTTPStatus reports the response status that failed.
func (e *StatusError) HTTPStatus() int {
	return e.Status
}

// HTTPStatusError is implemented by provider errors that carry a response
// status, letting Splittable classify failures without sharing one error type.
type HTTPStatusError interface {
	error
	HTTPStatus() int
}

// StatusOf reports the HTTP status carried by err, if any.
func StatusOf(err error) (int, bool) {
	var statusErr HTTPStatusError
	if errors.As(err, &statusErr) {
		return statusErr.HTTPStatus(), true
	}
	return 0, false
}

// permanentError marks a failure that repeating a request cannot fix.
type permanentError struct {
	err error
}

func (e permanentError) Error() string {
	return e.err.Error()
}

func (e permanentError) Unwrap() error {
	return e.err
}

// Permanent marks err as unretryable. Callers use it for local failures such as
// an unencodable request or an undecodable response, which a smaller batch
// would repeat unchanged.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return permanentError{err: err}
}

// Policy bounds one retryable operation.
type Policy struct {
	// MaxAttempts includes the first attempt.
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

// Default retries a lookup twice after its first attempt with exponential
// backoff. A server-provided Retry-After wins over the computed delay.
var Default = Policy{
	MaxAttempts: 3,
	BaseDelay:   200 * time.Millisecond,
	MaxDelay:    2 * time.Second,
}

// maxRetryAfter keeps one throttled response from blocking a lookup far longer
// than its retry budget, while still honoring the server's requested delay.
const maxRetryAfter = 30 * time.Second

// MaxSplitDepth bounds how many times a failed lookup batch is bisected.
// Three levels turn one 100-item failure into at most eight parts, keeping the
// work spent isolating a persistent failure bounded.
const MaxSplitDepth = 3

// Attempt is one lookup attempt's outcome. A nil Err with a non-transient
// Status is final. A zero Status means no HTTP response arrived.
type Attempt struct {
	Status     int
	RetryAfter time.Duration
	Err        error
}

// TransientStatus reports whether a read-only request status is worth
// retrying: a rate limit or a server-side failure.
func TransientStatus(status int) bool {
	return status == http.StatusTooManyRequests ||
		(status >= 500 && status <= 599)
}

// Do runs attempt until it returns a final outcome, the policy exhausts its
// attempts, or ctx ends. A transport error is retried like a transient status;
// a final attempt's outcome is returned unchanged so its status stays
// available to the caller.
func Do(
	ctx context.Context,
	policy Policy,
	attempt func(context.Context) Attempt,
) Attempt {
	policy = policy.normalized()
	var last Attempt
	for number := 1; number <= policy.MaxAttempts; number++ {
		if err := ctx.Err(); err != nil {
			return Attempt{Err: err}
		}
		last = attempt(ctx)
		if _, permanent := errors.AsType[permanentError](last.Err); permanent {
			return last
		}
		if last.Err == nil && !TransientStatus(last.Status) {
			return last
		}
		if number == policy.MaxAttempts {
			return last
		}
		if err := wait(ctx, policy.delay(number, last.RetryAfter)); err != nil {
			return Attempt{Err: err}
		}
	}
	return last
}

// RetryAfter parses the Retry-After header as either delay-seconds or an HTTP
// date. A missing, past, or malformed value reports no requested delay.
func RetryAfter(header http.Header) time.Duration {
	value := strings.TrimSpace(header.Get("Retry-After"))
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
		return time.Duration(min(seconds, int(maxRetryAfter/time.Second))) * time.Second
	}
	if at, err := http.ParseTime(value); err == nil {
		if delay := time.Until(at); delay > 0 {
			return delay
		}
	}
	return 0
}

// Splittable reports whether a failed batch lookup may be retried per part.
// Permanent statuses and caller cancellation are final; every other failure,
// including a wrapped transport error, may still succeed for a smaller batch.
func Splittable(err error) bool {
	if err == nil ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var permanent permanentError
	if errors.As(err, &permanent) {
		return false
	}
	if status, ok := StatusOf(err); ok {
		return TransientStatus(status)
	}
	return true
}

func (p Policy) normalized() Policy {
	if p.MaxAttempts < 1 {
		p.MaxAttempts = 1
	}
	if p.BaseDelay <= 0 {
		p.BaseDelay = Default.BaseDelay
	}
	if p.MaxDelay <= 0 {
		p.MaxDelay = Default.MaxDelay
	}
	if p.MaxDelay < p.BaseDelay {
		p.MaxDelay = p.BaseDelay
	}
	return p
}

func (p Policy) delay(number int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		if retryAfter > maxRetryAfter {
			return maxRetryAfter
		}
		return retryAfter
	}
	delay := p.BaseDelay
	for step := 1; step < number && delay < p.MaxDelay; step++ {
		delay *= 2
	}
	if delay > p.MaxDelay {
		return p.MaxDelay
	}
	return delay
}

func wait(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
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
