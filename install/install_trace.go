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
	// PackageCount receives the current lock's package count, the bounded
	// workload the operation starts from. It is reported only for an observed
	// lock; an absent lock reports nothing rather than a bogus zero, while an
	// observed empty lock reports a measured zero.
	PackageCount func(lockedPackages int)
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

// ReportPhase publishes a finished stage another package ran.
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

// reportPackageCount measures the starting workload from the lock the caller
// already holds. It reports nothing when no lock was observed, so an absent
// document is not presented as a measured zero, and it never walks the
// manifest for this observation. Installation reports before it resolves or
// replays, so a failure or cancellation still carries the starting scale.
func (t *Trace) reportPackageCount(old *document.Lock) {
	if t == nil || t.PackageCount == nil || old == nil {
		return
	}
	t.PackageCount(len(old.Packages))
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
