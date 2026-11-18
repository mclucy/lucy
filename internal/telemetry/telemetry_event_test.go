package telemetry

import (
	"encoding/json/v2"
	"testing"
	"time"
)

func TestRecordEventDurationPayload(t *testing.T) {
	t.Setenv(EnvironmentVariable, "1")
	now := time.Date(2026, time.November, 18, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		duration    time.Duration
		measured    bool
		wantPresent bool
		wantMS      int64
	}{
		{
			name:        "unmeasured zero is omitted",
			duration:    0,
			measured:    false,
			wantPresent: false,
		},
		{
			name:        "measured zero is emitted",
			duration:    0,
			measured:    true,
			wantPresent: true,
			wantMS:      0,
		},
		{
			name:        "positive submillisecond duration is emitted as zero",
			duration:    500 * time.Microsecond,
			measured:    false,
			wantPresent: true,
			wantMS:      0,
		},
		{
			name:        "positive duration truncates to whole milliseconds",
			duration:    2*time.Millisecond + 500*time.Microsecond,
			measured:    false,
			wantPresent: true,
			wantMS:      2,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, err := New(Config{
				Directory: t.TempDir(),
				Channel:   "alpha",
				Now:       func() time.Time { return now },
			})
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()

			submission, err := client.RecordEvent(t.Context(), Event{
				InvocationID:     "11111111-1111-4111-8111-111111111111",
				Command:          "java list",
				Duration:         test.duration,
				DurationMeasured: test.measured,
				Outcome:          OutcomeSuccess,
			})
			if err != nil {
				t.Fatalf("record event: %v", err)
			}
			if !submission.Queued {
				t.Fatalf("event was not queued: %+v", submission)
			}
			record, err := client.readQueueRecord(submission.RecordID)
			if err != nil {
				t.Fatalf("read outbox record: %v", err)
			}
			var payload eventPayload
			if err := json.Unmarshal(record.Payload, &payload); err != nil {
				t.Fatalf("decode outbox payload: %v", err)
			}
			if !test.wantPresent {
				if payload.DurationMS != nil {
					t.Fatalf("duration_ms = %d, want omitted", *payload.DurationMS)
				}
				return
			}
			if payload.DurationMS == nil {
				t.Fatal("duration_ms was omitted, want a measured value")
			}
			if got := *payload.DurationMS; got != test.wantMS {
				t.Fatalf("duration_ms = %d, want %d", got, test.wantMS)
			}
		})
	}
}
