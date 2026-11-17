package telemetry

import "testing"

func TestDisabledClientRefusesReportsAndUploads(t *testing.T) {
	client, err := New(Config{Directory: t.TempDir(), Disabled: true})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	result, err := client.SubmitReport(
		t.Context(),
		Report{Kind: ReportAgentFeedback, Body: "A command failed."},
	)
	if err != nil || result.Queued || result.Reason != SkipDisabled {
		t.Fatalf("disabled report: result=%+v err=%v", result, err)
	}
	flush, err := client.Flush(t.Context())
	if err != nil || !flush.Skipped || flush.Reason != SkipDisabled {
		t.Fatalf("disabled flush: result=%+v err=%v", flush, err)
	}
	stats, err := client.Stats(t.Context())
	if err != nil || stats.Enabled || stats.Reason != SkipDisabled {
		t.Fatalf("disabled stats: stats=%+v err=%v", stats, err)
	}
}
