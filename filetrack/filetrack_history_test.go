package filetrack

import (
	"testing"
	"time"

	documentpkg "github.com/mclucy/lucy/document"
)

func historyEvent(hash string) Event {
	return Event{
		Sequence:  1,
		Time:      time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		ID:        "event-1",
		Operation: Track,
		Changes: []Change{
			{
				Path:     "a.txt",
				After:    &documentpkg.TrackedFile{Path: "a.txt", Hash: hash},
				Accepted: true,
				Revision: 1,
				Mode:     0o644,
			},
		},
	}
}

func cloneHistoryEvent(event Event) Event {
	clone := event
	clone.Changes = make([]Change, len(event.Changes))
	for i, change := range event.Changes {
		clone.Changes[i] = change
		if change.Before != nil {
			before := *change.Before
			clone.Changes[i].Before = &before
		}
		if change.After != nil {
			after := *change.After
			clone.Changes[i].After = &after
		}
	}
	return clone
}

func TestValidateEvent(t *testing.T) {
	t.Parallel()
	hash := behaviorDigest([]byte("accepted contents"))
	valid := historyEvent(hash)
	next := cloneHistoryEvent(valid)
	next.Sequence = 2
	next.ID = "event-2"
	next.Changes[0].Revision = 2
	next.Changes[0].Parent = 1
	next.Changes[0].Mode = 0o600
	next.Changes[0].After.Hash = behaviorDigest([]byte("revised contents"))

	tests := []struct {
		name     string
		previous []Event
		event    Event
		wantErr  bool
	}{
		{name: "valid first event", event: valid},
		{name: "valid next event", previous: []Event{valid}, event: next},
		{
			name:    "sequence mismatch",
			event:   func() Event { event := cloneHistoryEvent(valid); event.Sequence = 2; return event }(),
			wantErr: true,
		},
		{
			name:    "empty identity",
			event:   func() Event { event := cloneHistoryEvent(valid); event.ID = ""; return event }(),
			wantErr: true,
		},
		{
			name:    "zero time",
			event:   func() Event { event := cloneHistoryEvent(valid); event.Time = time.Time{}; return event }(),
			wantErr: true,
		},
		{
			name: "non-UTC time",
			event: func() Event {
				event := cloneHistoryEvent(valid)
				event.Time = event.Time.In(time.FixedZone("x", 3600))
				return event
			}(),
			wantErr: true,
		},
		{
			name:    "no changes",
			event:   func() Event { event := cloneHistoryEvent(valid); event.Changes = nil; return event }(),
			wantErr: true,
		},
		{
			name:    "unknown operation",
			event:   func() Event { event := cloneHistoryEvent(valid); event.Operation = "unknown"; return event }(),
			wantErr: true,
		},
		{
			name: "duplicate path",
			event: func() Event {
				event := cloneHistoryEvent(valid)
				event.Changes = append(event.Changes, event.Changes[0])
				return event
			}(),
			wantErr: true,
		},
		{
			name: "traversal path",
			event: func() Event {
				event := cloneHistoryEvent(valid)
				event.Changes[0].Path = "../outside.txt"
				event.Changes[0].After.Path = "../outside.txt"
				return event
			}(),
			wantErr: true,
		},
		{
			name: "parent out of range",
			event: func() Event {
				event := cloneHistoryEvent(valid)
				event.Changes[0].Parent = 1
				return event
			}(),
			wantErr: true,
		},
		{
			name: "revision out of range",
			event: func() Event {
				event := cloneHistoryEvent(valid)
				event.Changes[0].Accepted = false
				event.Changes[0].Revision = 2
				return event
			}(),
			wantErr: true,
		},
		{
			name: "mode bits",
			event: func() Event {
				event := cloneHistoryEvent(valid)
				event.Changes[0].Mode = 0o1000
				return event
			}(),
			wantErr: true,
		},
		{
			name: "invalid backup hash",
			event: func() Event {
				event := cloneHistoryEvent(valid)
				event.Changes[0].BackupHash = "sha256:invalid"
				return event
			}(),
			wantErr: true,
		},
		{
			name: "accepted untrack",
			event: func() Event {
				event := cloneHistoryEvent(valid)
				event.Operation = Untrack
				return event
			}(),
			wantErr: true,
		},
		{
			name: "accepted without after",
			event: func() Event {
				event := cloneHistoryEvent(valid)
				event.Changes[0].After = nil
				return event
			}(),
			wantErr: true,
		},
		{
			name: "accepted without hash",
			event: func() Event {
				event := cloneHistoryEvent(valid)
				event.Changes[0].After.Hash = ""
				return event
			}(),
			wantErr: true,
		},
		{
			name: "accepted wrong revision",
			event: func() Event {
				event := cloneHistoryEvent(valid)
				event.Changes[0].Revision = 2
				return event
			}(),
			wantErr: true,
		},
		{
			name: "accepted missing parent",
			previous: []Event{
				func() Event {
					event := cloneHistoryEvent(valid)
					event.Changes[0].Path = "b.txt"
					event.Changes[0].After.Path = "b.txt"
					return event
				}(),
			},
			event: func() Event {
				event := cloneHistoryEvent(next)
				event.Changes[0].Parent = 1
				return event
			}(),
			wantErr: true,
		},
		{
			name: "non-accepted missing revision",
			event: func() Event {
				event := cloneHistoryEvent(valid)
				event.Changes[0].Accepted = false
				event.Changes[0].Revision = 1
				return event
			}(),
			wantErr: true,
		},
		{
			name: "declaration path mismatch",
			event: func() Event {
				event := cloneHistoryEvent(valid)
				event.Changes[0].After.Path = "b.txt"
				return event
			}(),
			wantErr: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := validateEvent(test.previous, test.event)
			if test.wantErr && err == nil {
				t.Fatalf("validateEvent accepted %+v", test.event)
			}
			if !test.wantErr && err != nil {
				t.Fatalf("validateEvent(%+v) = %v", test.event, err)
			}
		})
	}
}

func TestHistoryRevisionNavigation(t *testing.T) {
	t.Parallel()
	firstHash := behaviorDigest([]byte("first revision"))
	secondHash := behaviorDigest([]byte("second revision"))
	first := historyEvent(firstHash)
	second := cloneHistoryEvent(first)
	second.Sequence = 2
	second.ID = "event-2"
	second.Changes[0].Revision = 2
	second.Changes[0].Parent = 1
	second.Changes[0].After.Hash = secondHash
	events := []Event{first, second}

	revision, ok := revisionAt(events, "a.txt", 1)
	if !ok || revision.hash != firstHash || revision.parent != 0 {
		t.Fatalf("revisionAt(1) = %+v, %v", revision, ok)
	}
	revision, ok = revisionAt(events, "a.txt", 2)
	if !ok || revision.hash != secondHash || revision.parent != 1 {
		t.Fatalf("revisionAt(2) = %+v, %v", revision, ok)
	}
	if _, ok := revisionAt(events, "a.txt", 0); ok {
		t.Fatal("revisionAt(0) found a revision")
	}
	if _, ok := revisionAt(events, "a.txt", 3); ok {
		t.Fatal("revisionAt(3) found a revision")
	}
	if got := currentRevision(events, "a.txt", secondHash); got.sequence != 2 {
		t.Fatalf("currentRevision(second) = %+v", got)
	}
	if got := currentRevision(events, "a.txt", firstHash); got != (acceptedRevision{}) {
		t.Fatalf("currentRevision(first) = %+v, want no current revision", got)
	}

	revision, err := revertRevision(events, "a.txt", secondHash, "", 0)
	if err != nil || revision.hash != firstHash {
		t.Fatalf("previous revision = %+v, %v", revision, err)
	}
	revision, err = revertRevision(events, "a.txt", secondHash, firstHash, 0)
	if err != nil || revision.hash != firstHash {
		t.Fatalf("hash revision = %+v, %v", revision, err)
	}
	revision, err = revertRevision(events, "a.txt", secondHash, "", 1)
	if err != nil || revision.hash != firstHash {
		t.Fatalf("sequence revision = %+v, %v", revision, err)
	}
	if _, err := revertRevision(events, "a.txt", secondHash, firstHash, 1); err == nil {
		t.Fatal("revertRevision accepted hash and revision together")
	}
	if _, err := revertRevision(events, "a.txt", secondHash, behaviorDigest([]byte("missing")), 0); err == nil {
		t.Fatal("revertRevision accepted an unknown hash")
	}
	if _, err := revertRevision(events, "a.txt", secondHash, "", 3); err == nil {
		t.Fatal("revertRevision accepted an unknown revision")
	}
	if _, err := revertRevision([]Event{first}, "a.txt", firstHash, "", 0); err == nil {
		t.Fatal("revertRevision accepted a revision without a parent")
	}
}
