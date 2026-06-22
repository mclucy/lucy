package install

import "github.com/mclucy/lucy/types"

// EventKind identifies a pipeline lifecycle event.
type EventKind uint8

const (
	EventBatchPhase EventKind = iota
	EventBatchSummary
	EventResolveStart
	EventDownloadStart
	EventVerifyStart
	EventReconcileStart
	EventReconcileDiff
	EventApplyStart
	EventConflict
)

// Event represents a pipeline lifecycle event.
type Event struct {
	Kind   EventKind
	Header string
	IDs    []types.VersionedPackageRef
	Count  int
	Failed int
	Roots  []types.VersionedPackageRef
	Diff   ReconcileDiff
	Err    error
}

// Journal records pipeline lifecycle events.
type Journal interface {
	Record(event Event)
}

func recordEvent(journal Journal, event Event) {
	if journal == nil {
		journal = logJournal{}
	}
	journal.Record(event)
}
