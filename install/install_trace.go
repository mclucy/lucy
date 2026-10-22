package install

import (
	"io"
	"time"

	"github.com/mclucy/lucy/resolve"
	"github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/version"
)

// PhaseInfo describes a finished stage. Counts are facts the renderer turns
// into prose; the backend never formats them.
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
// optional: a nil field or a nil Trace reports nothing. The caller that
// installs a Trace presents the facts; this package never formats them.
type Trace struct {
	PhaseStarted  func(name string)
	PhaseDone     func(phase PhaseInfo)
	FetchStarted  func(name string, size int64)
	FetchProgress func(name string, read, total int64)
	FetchDone     func(name string, fromCache bool)
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
