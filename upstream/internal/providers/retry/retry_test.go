package retry

import (
	"context"
	"errors"
	"math"
	"net/http"
	"testing"
	"time"
)

func TestRetryAfterPreservesRequestedDelay(t *testing.T) {
	t.Parallel()
	header := http.Header{"Retry-After": []string{"120"}}
	if got := RetryAfter(header); got != 120*time.Second {
		t.Fatalf("Retry-After = %s, want 2m", got)
	}
}

func TestRetryAfterNumericOverflow(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		value string
		want  time.Duration
	}{
		{
			name:  "overflows duration",
			value: "9223372037",
			want:  time.Duration(math.MaxInt64),
		},
		{
			name:  "overflows uint64",
			value: "99999999999999999999999999",
			want:  time.Duration(math.MaxInt64),
		},
		{
			name:  "invalid suffix after overflow",
			value: "99999999999999999999999999x",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			header := http.Header{"Retry-After": []string{tc.value}}
			if got := RetryAfter(header); got != tc.want {
				t.Fatalf("Retry-After = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestRetryAfterPastDateRequestsNoDelay(t *testing.T) {
	t.Parallel()
	header := http.Header{}
	header.Set(
		"Retry-After",
		time.Now().Add(-time.Minute).UTC().Format(http.TimeFormat),
	)
	if got := RetryAfter(header); got != 0 {
		t.Fatalf("past Retry-After = %s, want 0", got)
	}
}

func TestDoStopsRatherThanShorteningThrottleBeyondBudget(t *testing.T) {
	t.Parallel()
	attempts := 0
	requested := Default.MaxThrottleWait + time.Second
	outcome := Do(t.Context(), Default, func(context.Context) Attempt {
		attempts++
		return Attempt{
			Status:     http.StatusTooManyRequests,
			RetryAfter: requested,
		}
	})

	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
	if outcome.Status != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", outcome.Status)
	}
	if outcome.RetryAfter != requested {
		t.Fatalf("retry after = %s, want the requested %s", outcome.RetryAfter, requested)
	}
}

func TestDoStopsWhenCumulativeThrottleBudgetIsSpent(t *testing.T) {
	t.Parallel()
	policy := Policy{
		MaxAttempts:     5,
		BaseDelay:       time.Millisecond,
		MaxDelay:        time.Millisecond,
		MaxThrottleWait: 30 * time.Millisecond,
	}
	attempts := 0
	outcome := Do(t.Context(), policy, func(context.Context) Attempt {
		attempts++
		return Attempt{
			Status:     http.StatusTooManyRequests,
			RetryAfter: 20 * time.Millisecond,
		}
	})

	if attempts != 2 {
		t.Fatalf(
			"attempts = %d, want 2 (two 20ms waits exceed a 30ms budget)",
			attempts,
		)
	}
	if outcome.Status != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", outcome.Status)
	}
}

func TestDoRetriesTransientStatusWithoutBodyError(t *testing.T) {
	t.Parallel()
	policy := Policy{
		MaxAttempts: 3,
		BaseDelay:   time.Millisecond,
		MaxDelay:    time.Millisecond,
	}
	attempts := 0
	outcome := Do(t.Context(), policy, func(context.Context) Attempt {
		attempts++
		if attempts == 1 {
			return Attempt{Status: http.StatusServiceUnavailable}
		}
		return Attempt{Status: http.StatusOK}
	})

	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if outcome.Status != http.StatusOK || outcome.Err != nil {
		t.Fatalf("outcome = %+v, want a successful retry", outcome)
	}
}

func TestDoRetriesSuccessBodyReadError(t *testing.T) {
	t.Parallel()
	policy := Policy{
		MaxAttempts: 3,
		BaseDelay:   time.Millisecond,
		MaxDelay:    time.Millisecond,
	}
	readErr := errors.New("body read failed")
	attempts := 0
	outcome := Do(t.Context(), policy, func(context.Context) Attempt {
		attempts++
		if attempts == 1 {
			return Attempt{Status: http.StatusOK, Err: readErr}
		}
		return Attempt{Status: http.StatusOK}
	})

	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if outcome.Status != http.StatusOK || outcome.Err != nil {
		t.Fatalf("outcome = %+v, want a successful retry", outcome)
	}
}

func TestDoStopsThrottleBeyondContextDeadline(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	attempts := 0
	started := time.Now()
	outcome := Do(ctx, Default, func(context.Context) Attempt {
		attempts++
		return Attempt{
			Status:     http.StatusTooManyRequests,
			RetryAfter: 5 * time.Second,
		}
	})

	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("waited %s, want to stop instead of shortening the delay", elapsed)
	}
	if outcome.Status != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", outcome.Status)
	}
}

func TestDoKeepsDefinitiveStatusAfterBodyReadError(t *testing.T) {
	t.Parallel()
	attempts := 0
	readErr := errors.New("body read failed")
	outcome := Do(t.Context(), Default, func(context.Context) Attempt {
		attempts++
		return Attempt{Status: http.StatusNotFound, Err: readErr}
	})

	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
	if outcome.Status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", outcome.Status)
	}
	if !errors.Is(outcome.Err, readErr) {
		t.Fatalf("error = %v, want the body read failure", outcome.Err)
	}
}

func TestSplittableThrottleAndTransientStatuses(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "throttle with delay",
			err: &ThrottledError{
				Status:     http.StatusTooManyRequests,
				RetryAfter: time.Second,
			},
			want: false,
		},
		{
			name: "server failure with delay",
			err: &ThrottledError{
				Status:     http.StatusServiceUnavailable,
				RetryAfter: time.Second,
			},
			want: false,
		},
		{
			name: "rate limit without delay",
			err:  &StatusError{Status: http.StatusTooManyRequests},
			want: true,
		},
		{
			name: "server failure",
			err:  &StatusError{Status: http.StatusServiceUnavailable},
			want: true,
		},
		{
			name: "permanent failure",
			err:  Permanent(errors.New("invalid response")),
			want: false,
		},
		{
			name: "cancellation",
			err:  context.Canceled,
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := Splittable(tc.err); got != tc.want {
				t.Fatalf("Splittable(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
