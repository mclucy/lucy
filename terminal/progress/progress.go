// Package progress provides a terminal progress bar backed by the charm stack
// (bubbletea + bubbles/progress + lipgloss).
//
// Unlike the parent tui package which is a one-shot static renderer, this
// package uses bubbletea for live, interactive progress display.
//
// # Lifecycle Invariants
//
// The progress runtime does not call os.Exit or terminate the process.
// The caller controls process lifecycle. On interrupt (Ctrl+C), the runtime
// sets an internal stopped flag and returns control to the caller.
//
// Close() and Complete() are idempotent and mark entries as completed.
// The renderer stops only when all registered entries have completed.
// After all-complete shutdown, registering a new tracker resets the stopped
// flag and restarts the renderer.
//
// Usage:
//
//	t := progress.NewTracker("Downloading")
//	go func() {
//	    defer t.Close()
//	    resp, _ := http.Get(url)
//	    reader := t.ProxyReader(resp.Body, resp.ContentLength)
//	    io.Copy(dst, reader)
//	}()
package progress

import (
	"context"
	"io"
)

// Tracker is a thread-safe progress bar controller.
//
// A Tracker is created with [NewTracker] and automatically starts displaying.
// External goroutines update progress via [Tracker.SetPercent],
// [Tracker.IncrPercent], and [Tracker.SetMessage].
// Call [Tracker.Close] to mark the tracker as completed.
type Tracker struct {
	id          entryID
	logCapacity int
}

// NewTracker creates a [Tracker] with the given title and starts displaying it.
// Log capacity defaults to 5 lines.
func NewTracker(title string) *Tracker {
	return newTracker(title, 5)
}

// NewTrackerWithLogging creates a [Tracker] with custom log line capacity.
func NewTrackerWithLogging(title string, logLimit int) *Tracker {
	return newTracker(title, logLimit)
}

func newTracker(title string, logCapacity int) *Tracker {
	id := globalRuntime.registerEntry(title, logCapacity)
	return &Tracker{id: id, logCapacity: logCapacity}
}

// SetPercent sets the current progress to p (clamped to [0, 1]).
func (t *Tracker) SetPercent(p float64) {
	globalRuntime.send(t.id, setPercentMsg(clamp01(p)))
}

// IncrPercent adds delta to the current progress.
func (t *Tracker) IncrPercent(delta float64) {
	globalRuntime.send(t.id, incrPercentMsg(delta))
}

// SetMessage updates the status text shown alongside the bar.
func (t *Tracker) SetMessage(msg string) {
	globalRuntime.send(t.id, setMessageMsg(msg))
}

// SetTitle updates the title shown at the top of the progress bar.
func (t *Tracker) SetTitle(title string) {
	globalRuntime.send(t.id, setTitleMsg(title))
}

// Close marks this tracker as completed. The renderer stops only when all
// registered trackers are completed. Close is idempotent.
func (t *Tracker) Close() {
	globalRuntime.send(t.id, closeMsg{})
}

// Complete marks this tracker as completed and sets the progress to 100%
// with a completion message. Like Close, the renderer stops only when all
// registered trackers are completed.
func (t *Tracker) Complete(msg string) {
	globalRuntime.send(t.id, completeMsg(msg))
}

func (t *Tracker) CacheHit() {
	t.Complete("Cache hit")
}

// ProxyReader wraps r so that every Read call updates this Tracker.
// total is the expected total byte count (e.g. from Content-Length).
// If total <= 0 the bar will not be updated (indeterminate).
func (t *Tracker) ProxyReader(r io.Reader, total int64) io.Reader {
	return &proxyReader{Reader: r, tracker: t, total: total}
}

// LogWriter returns an io.Writer that ingests streaming bytes, splits by
// newline, and sends log-update messages to runtime. Partial fragments are
// preserved between writes and only complete lines are emitted unless the
// runtime explicitly flushes on close.
func (t *Tracker) LogWriter() io.Writer {
	return &logWriter{tracker: t}
}

// setBytesProgress is an internal method used by proxyReader to send
// byte-level progress updates to the model.
func (t *Tracker) setBytesProgress(read, total int64) {
	globalRuntime.send(t.id, bytesProgressMsg{read: read, total: total})
}

// appendLog is an internal method used by logWriter to send log data to the
// runtime. The runtime handles newline splitting and partial-line buffering.
func (t *Tracker) appendLog(data string) {
	globalRuntime.send(t.id, appendLogMsg(data))
}

// WaitForShutdown blocks until the progress runtime completes teardown or ctx expires.
// Returns nil if runtime completes successfully, ctx.Err() on timeout/cancellation.
// Returns immediately if runtime is not active.
func WaitForShutdown(ctx context.Context) error {
	return globalRuntime.waitForShutdown(ctx)
}
