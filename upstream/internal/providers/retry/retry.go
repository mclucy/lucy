// Package retry performs bounded retries with backoff for read-only provider
// lookups. It never wraps a mutation: callers use it only for requests whose
// repetition has no side effect.
package retry

import (
	"context"
	"errors"
	"fmt"
	"math"
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

// ThrottledError reports a lookup the provider asked callers to delay before
// asking again. Callers stop the affected lookup instead of shortening the
// delay or retrying a smaller batch early. It is only created when the
// provider asked for a delay; a transient status without one stays an
// ordinary retryable failure.
type ThrottledError struct {
	Status     int
	RetryAfter time.Duration
}

func (e *ThrottledError) Error() string {
	return fmt.Sprintf(
		"status %d: throttled, retry after %s",
		e.Status,
		e.RetryAfter,
	)
}

// HTTPStatus reports the throttle status so batch splitting can classify it.
func (e *ThrottledError) HTTPStatus() int {
	return e.Status
}

// Throttled reports whether err is a rate limit that asked for a delay. Such a
// failure ends the affected lookup: splitting it or starting the next batch
// would reach the provider before the delay it requested.
func Throttled(err error) bool {
	var throttled *ThrottledError
	return errors.As(err, &throttled)
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
	// MaxThrottleWait bounds the total server-requested delay one operation
	// may spend. Zero uses the default.
	MaxThrottleWait time.Duration
}

// Default retries a lookup twice after its first attempt with exponential
// backoff. A server-provided Retry-After wins over the computed delay.
var Default = Policy{
	MaxAttempts:     3,
	BaseDelay:       200 * time.Millisecond,
	MaxDelay:        2 * time.Second,
	MaxThrottleWait: defaultMaxThrottleWait,
}

// defaultMaxThrottleWait bounds the cumulative server-requested delay one
// retry operation may spend. A delay that would exceed it is never silently
// shortened: the lookup stops with the throttled outcome instead, so no
// request reaches the provider before the delay it requested.
const defaultMaxThrottleWait = 30 * time.Second

// MaxSplitDepth bounds how many times a failed lookup batch is bisected.
// Three levels turn one 100-item failure into at most eight parts, keeping the
// work spent isolating a persistent failure bounded.
const MaxSplitDepth = 3

// Attempt is one lookup outcome. A non-transient HTTP failure is final even
// if the response body failed to read. A zero Status means no response arrived.
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
// attempts, or ctx ends. A transport error is retried like a transient status.
// A non-transient HTTP failure stays final even if reading its body failed.
// A requested throttle delay is honored as given. If the remaining budget
// cannot cover it, the lookup stops rather than retrying early.
func Do(
	ctx context.Context,
	policy Policy,
	attempt func(context.Context) Attempt,
) Attempt {
	policy = policy.normalized()
	var last Attempt
	var throttleWaited time.Duration
	for number := 1; number <= policy.MaxAttempts; number++ {
		if err := ctx.Err(); err != nil {
			return Attempt{Err: err}
		}
		last = attempt(ctx)
		if _, permanent := errors.AsType[permanentError](last.Err); permanent {
			return last
		}
		httpFailure := last.Status != 0 && (last.Status < 200 || last.Status >= 300)
		if !TransientStatus(last.Status) && (last.Err == nil || httpFailure) {
			return last
		}
		if number == policy.MaxAttempts {
			return last
		}
		if !policy.fitsThrottleWait(ctx, throttleWaited, last.RetryAfter) {
			return last
		}
		throttleWaited += last.RetryAfter
		if err := wait(ctx, policy.delay(number, last.RetryAfter)); err != nil {
			return Attempt{Err: err}
		}
	}
	return last
}

// fitsThrottleWait reports whether a requested delay fits the operation's
// cumulative throttle budget and the context deadline. Zero means the attempt
// requested no delay.
func (p Policy) fitsThrottleWait(
	ctx context.Context,
	waited, requested time.Duration,
) bool {
	if requested <= 0 {
		return true
	}
	if requested > p.MaxThrottleWait-waited {
		return false
	}
	if deadline, ok := ctx.Deadline(); ok && requested >= time.Until(deadline) {
		return false
	}
	return true
}

// RetryAfter parses the Retry-After header as either delay-seconds or an HTTP
// date. A missing, past, or malformed value reports no requested delay. The
// parsed delay is returned as given: a value beyond the operation budget stops
// the lookup in Do rather than being shortened here. A numeric value too large
// for the duration type saturates instead of reading as absent.
func RetryAfter(header http.Header) time.Duration {
	value := strings.TrimSpace(header.Get("Retry-After"))
	if value == "" {
		return 0
	}
	seconds, err := strconv.ParseUint(value, 10, 64)
	if err == nil && seconds > 0 {
		const maxSeconds = uint64(math.MaxInt64 / time.Second)
		if seconds > maxSeconds {
			// Parsed seconds fit uint64 but exceed what time.Duration can
			// represent after scaling; saturate.
			return time.Duration(math.MaxInt64)
		}
		return time.Duration(seconds) * time.Second
	}
	if errors.Is(err, strconv.ErrRange) {
		// Syntactically valid digits overflowed; the provider asked to wait
		// longer than any duration can express, so saturate.
		return time.Duration(math.MaxInt64)
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
// A rate limit that asked for a delay is final too: splitting it would reach
// the provider before that delay. A rate limit without a delay stays
// splittable like any other transient status.
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
	if Throttled(err) {
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
	if p.MaxThrottleWait <= 0 {
		p.MaxThrottleWait = Default.MaxThrottleWait
	}
	return p
}

func (p Policy) delay(number int, retryAfter time.Duration) time.Duration {
	// A server-requested delay is returned as given. Do decides whether it
	// fits the operation budget before waiting.
	if retryAfter > 0 {
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
