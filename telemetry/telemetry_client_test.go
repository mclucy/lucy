package telemetry

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestDisabledClientDoesNotRecordOrUpload(t *testing.T) {
	t.Setenv(EnvironmentVariable, "")
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := Config{Directory: t.TempDir(), Endpoint: server.URL, Channel: "alpha"}
	release, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer release.Close()
	if err := release.SetEnabled(t.Context(), true); err != nil {
		t.Fatal(err)
	}
	result, err := release.SubmitReport(t.Context(), Report{Kind: ReportAgentFeedback, Body: "A command failed."})
	if err != nil || !result.Queued {
		t.Fatalf("queue release report: result=%+v err=%v", result, err)
	}

	config.Disabled = true
	local, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer local.Close()
	result, err = local.SubmitReport(t.Context(), Report{Kind: ReportAgentFeedback, Body: "Another command failed."})
	if err != nil || result.Queued || result.Reason != SkipDisabled {
		t.Fatalf("local report: result=%+v err=%v", result, err)
	}
	flush, err := local.Flush(t.Context())
	if err != nil || !flush.Skipped || requests.Load() != 0 {
		t.Fatalf("local flush: result=%+v requests=%d err=%v", flush, requests.Load(), err)
	}
	stats, err := local.Stats(t.Context())
	if err != nil || stats.Enabled || stats.Queue.Reports != 1 {
		t.Fatalf("local stats: stats=%+v err=%v", stats, err)
	}
}
