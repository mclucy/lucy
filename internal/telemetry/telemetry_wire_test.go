package telemetry

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (roundTrip roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

type truncatedBody struct {
	remaining string
}

func (body *truncatedBody) Read(buffer []byte) (int, error) {
	if body.remaining == "" {
		return 0, errors.New("truncated response body")
	}
	count := copy(buffer, body.remaining)
	body.remaining = body.remaining[count:]
	return count, nil
}

func (body *truncatedBody) Close() error { return nil }

func testHTTPResponse(status int, statusText string, body io.ReadCloser) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     statusText,
		Header:     make(http.Header),
		Body:       body,
	}
}

func TestFlushThrottleForUnreadableResponse(t *testing.T) {
	t.Setenv(EnvironmentVariable, "1")
	const maxAttempts = 2
	tests := []struct {
		name          string
		status        int
		statusText    string
		wantThrottles int64
	}{
		{
			name:          "429 counts as throttle",
			status:        http.StatusTooManyRequests,
			statusText:    "429 Too Many Requests",
			wantThrottles: maxAttempts,
		},
		{
			name:          "other statuses do not",
			status:        http.StatusInternalServerError,
			statusText:    "500 Internal Server Error",
			wantThrottles: 0,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			now := time.Date(2026, time.November, 18, 12, 0, 0, 0, time.UTC)
			posts := 0
			transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
				switch {
				case request.Method == http.MethodGet && request.URL.Path == "/health":
					return testHTTPResponse(http.StatusOK, "200 OK", io.NopCloser(strings.NewReader("{}"))), nil
				case request.Method == http.MethodPost && request.URL.Path == "/v1/records":
					posts++
					return testHTTPResponse(test.status, test.statusText, &truncatedBody{remaining: `{"accepted":["`}), nil
				default:
					return nil, fmt.Errorf("unexpected request %s %s", request.Method, request.URL.Path)
				}
			})
			client, err := New(Config{
				Directory:  t.TempDir(),
				Endpoint:   "http://127.0.0.1:1",
				Channel:    "alpha",
				Now:        func() time.Time { return now },
				HTTPClient: &http.Client{Transport: transport},
				Limits: Limits{
					MaxAttempts:  maxAttempts,
					RetryBackoff: time.Millisecond,
					MaxBatches:   1,
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()

			submission, err := client.RecordEvent(t.Context(), Event{
				InvocationID:     "22222222-2222-4222-8222-222222222222",
				Operation:        "java list",
				DurationMeasured: true,
				Outcome:          OutcomeSuccess,
			})
			if err != nil {
				t.Fatalf("record event: %v", err)
			}
			if !submission.Queued {
				t.Fatalf("event was not queued: %+v", submission)
			}

			result, err := client.Flush(t.Context())
			if err != nil {
				t.Fatalf("flush: %v", err)
			}
			if posts != maxAttempts {
				t.Fatalf("POST attempts = %d, want %d retryable attempts", posts, maxAttempts)
			}
			if result.Attempted != 1 || result.Remaining != 1 {
				t.Fatalf("flush result = %+v, want one attempted and one remaining", result)
			}
			if result.Skipped {
				t.Fatalf("flush was skipped: %+v", result)
			}

			stats, err := client.Stats(t.Context())
			if err != nil {
				t.Fatalf("stats: %v", err)
			}
			if stats.Queue.Records != 1 || stats.Queue.Events != 1 {
				t.Fatalf("queue = %+v, want one untouched event", stats.Queue)
			}
			if stats.Transport.Requests != maxAttempts {
				t.Fatalf("transport requests = %d, want %d", stats.Transport.Requests, maxAttempts)
			}
			if stats.Transport.Uncertain != maxAttempts {
				t.Fatalf("transport uncertain = %d, want %d", stats.Transport.Uncertain, maxAttempts)
			}
			if stats.Transport.Throttles != test.wantThrottles {
				t.Fatalf("transport throttles = %d, want %d", stats.Transport.Throttles, test.wantThrottles)
			}
			if stats.Transport.RejectedRequests != 0 || stats.Transport.Succeeded != 0 {
				t.Fatalf("transport outcomes = %+v, want no success or rejection", stats.Transport)
			}
		})
	}
}
