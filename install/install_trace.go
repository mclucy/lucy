package install

import (
	"io"
	"time"

	"github.com/mclucy/lucy/bootstrap"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/resolve"
	"github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/version"
)

// PhaseInfo describes a finished stage.
type PhaseInfo struct {
	Name      string
	Elapsed   time.Duration
	Count     int
	Total     int
	Product   server.Product
	Version   version.Version
	Minecraft version.Version
}

// Trace receives facts about an operation while it runs. Every field is
// optional: a nil field or a nil Trace reports nothing.
type Trace struct {
	PhaseStarted  func(name string)
	PhaseProgress func(name string, count, total int)
	PhaseDone     func(phase PhaseInfo)
	FetchStarted  func(name string, size int64)
	FetchProgress func(name string, read, total int64)
	FetchDone     func(name string, fromCache bool)
	// InputScale receives the bounded workload the operation starts from: the
	// manifest's declared package count and the current lock's package count.
	// A negative count means the operation did not observe it; zero is a
	// measured empty set.
	InputScale func(requestedPackages, lockedPackages int)
	// ProcessOutput receives the output of subprocesses that the operation
	// runs, such as installer processors, Paperclip, and pip.
	ProcessOutput io.Writer
	Resolve       resolve.Trace
}

func (t *Trace) phaseStarted(name string) {
	if t != nil && t.PhaseStarted != nil {
		t.PhaseStarted(name)
	}
}

func (t *Trace) phaseDone(phase PhaseInfo) {
	if t != nil && t.PhaseDone != nil {
		t.PhaseDone(phase)
	}
}

func (t *Trace) progressPhase(name string, count, total int) {
	if t != nil && t.PhaseProgress != nil {
		t.PhaseProgress(name, count, total)
	}
}

// ReportPhaseProgress publishes bounded progress inside a started stage.
func (t *Trace) ReportPhaseProgress(name string, count, total int) {
	t.progressPhase(name, count, total)
}

// bootstrapProgress adapts the bootstrap package's callback shape to this
// operation's trace without either package importing the other.
func (t *Trace) bootstrapProgress() bootstrap.Progress {
	return bootstrap.Progress{
		PhaseStarted: func(name string) { t.phaseStarted(name) },
		PhaseProgress: func(name string, count, total int) {
			t.progressPhase(name, count, total)
		},
		PhaseDone: func(name string, elapsed time.Duration, count, total int) {
			t.phaseDone(PhaseInfo{
				Name:    name,
				Elapsed: elapsed,
				Count:   count,
				Total:   total,
			})
		},
	}
}

// ReportPhase publishes a finished stage another package ran. Initialization
// reports identification and adoption, and import reports the source download
// and the files it writes, which installation does not perform, so their
// elapsed time and counts are visible instead of unaccounted.
func (t *Trace) ReportPhase(phase PhaseInfo) {
	t.phaseDone(phase)
}

func (t *Trace) fetchStarted(name string, size int64) {
	if t != nil && t.FetchStarted != nil {
		t.FetchStarted(name, size)
	}
}

func (t *Trace) fetchProgress(name string, read, total int64) {
	if t != nil && t.FetchProgress != nil {
		t.FetchProgress(name, read, total)
	}
}

func (t *Trace) fetchDone(name string, fromCache bool) {
	if t != nil && t.FetchDone != nil {
		t.FetchDone(name, fromCache)
	}
}

// ReportInputScale publishes the workload the operation starts from. Counts
// the operation did not observe stay negative, so an absent document is not
// presented as a measured zero. Installation reports before it resolves or
// replays, so a failure or cancellation still carries the starting scale.
func (t *Trace) ReportInputScale(requestedPackages, lockedPackages int) {
	if t != nil && t.InputScale != nil {
		t.InputScale(requestedPackages, lockedPackages)
	}
}

// reportInputScale measures the starting workload from documents the caller
// already holds. Counting declared packages walks every requirement, so a
// Trace that observes no input scale does no measuring. The counts come from
// the manifest's declarations and the current lock, matching the bounded
// workload the operation is about to replay or resolve.
func (t *Trace) reportInputScale(d *document.Manifest, old *document.Lock) {
	if t == nil || t.InputScale == nil {
		return
	}
	requested, locked := -1, -1
	if d != nil {
		requested = countDeclared(d)
	}
	if old != nil {
		locked = len(old.Packages)
	}
	t.InputScale(requested, locked)
}

// ReportPhaseStarted publishes a stage another package began.
func (t *Trace) ReportPhaseStarted(name string) {
	t.phaseStarted(name)
}

// ReportFetchStarted publishes a transfer another package began.
func (t *Trace) ReportFetchStarted(name string, size int64) {
	t.fetchStarted(name, size)
}

// ReportFetchProgress updates the transfer's byte count.
func (t *Trace) ReportFetchProgress(name string, read, total int64) {
	t.fetchProgress(name, read, total)
}

// ReportFetchDone publishes that the transfer ended.
func (t *Trace) ReportFetchDone(name string, fromCache bool) {
	t.fetchDone(name, fromCache)
}

func (t *Trace) processOutput() io.Writer {
	if t == nil || t.ProcessOutput == nil {
		return io.Discard
	}
	return t.ProcessOutput
}

func (t *Trace) resolveTrace() resolve.Trace {
	if t == nil {
		return resolve.Trace{}
	}
	return t.Resolve
}
