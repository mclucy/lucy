package install

import "github.com/mclucy/lucy/types"

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

type Journal interface {
	Record(event Event)
}

func recordEvent(journal Journal, event Event) {
	if journal == nil {
		journal = logJournal{}
	}
	journal.Record(event)
}
