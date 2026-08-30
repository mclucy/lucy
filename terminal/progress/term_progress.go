// Package progress displays terminal progress bars.
//
// It uses Bubble Tea, Bubbles progress, and Lip Gloss.
// The runtime stops after all registered entries complete. Registering a
// tracker after shutdown starts the runtime again.
//
// Close and Complete are idempotent. Both mark an entry as complete.
// Pressing Ctrl+C exits the process with status 130.
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

// Tracker controls one progress entry. It is safe for concurrent use.
//
// NewTracker starts the progress runtime. Use SetPercent, IncrPercent, and
// SetMessage to update the entry. Call Close when the entry is complete.
type Tracker struct {
	id          entryID
	logCapacity int
}

// NewTracker creates a tracker with the given title.
// It keeps five log lines by default.
func NewTracker(title string) *Tracker {
	return newTracker(title, 5)
}

// NewTrackerWithLogging creates a tracker with the given log line limit.
func NewTrackerWithLogging(title string, logLimit int) *Tracker {
	return newTracker(title, logLimit)
}

func newTracker(title string, logCapacity int) *Tracker {
	id := globalRuntime.registerEntry(title, logCapacity)
	return &Tracker{id: id, logCapacity: logCapacity}
}

// SetPercent sets p as the current progress. It clamps p to [0, 1].
func (t *Tracker) SetPercent(p float64) {
	globalRuntime.send(t.id, setPercentMsg(clamp01(p)))
}

// IncrPercent adds delta to the current progress.
func (t *Tracker) IncrPercent(delta float64) {
	globalRuntime.send(t.id, incrPercentMsg(delta))
}

// SetMessage sets the status text shown next to the bar.
func (t *Tracker) SetMessage(msg string) {
	globalRuntime.send(t.id, setMessageMsg(msg))
}

// SetTitle sets the title shown above the progress bar.
func (t *Tracker) SetTitle(title string) {
	globalRuntime.send(t.id, setTitleMsg(title))
}

// Close marks this tracker as complete. It is safe to call more than once.
func (t *Tracker) Close() {
	globalRuntime.send(t.id, closeMsg{})
}

// Complete marks this tracker as complete, sets progress to 100%, and sets
// the completion message. It is safe to call more than once.
func (t *Tracker) Complete(msg string) {
	globalRuntime.send(t.id, completeMsg(msg))
}

// CacheHit marks this tracker complete with a cache hit message.
func (t *Tracker) CacheHit() {
	t.Complete("Cache hit")
}

// ProxyReader wraps r and updates this tracker after each read.
// total is the expected number of bytes. Values <= 0 disable byte progress.
func (t *Tracker) ProxyReader(r io.Reader, total int64) io.Reader {
	return &proxyReader{Reader: r, tracker: t, total: total}
}

// LogWriter returns a writer that sends complete log lines to the runtime.
// It keeps partial lines until the next write or runtime shutdown.
func (t *Tracker) LogWriter() io.Writer {
	return &logWriter{tracker: t}
}

// setBytesProgress sends byte progress to the runtime.
func (t *Tracker) setBytesProgress(read, total int64) {
	globalRuntime.send(t.id, bytesProgressMsg{read: read, total: total})
}

// appendLog sends log data to the runtime.
func (t *Tracker) appendLog(data string) {
	globalRuntime.send(t.id, appendLogMsg(data))
}

// WaitForShutdown waits for the runtime to stop or for ctx to expire.
// It returns nil when shutdown completes and ctx.Err() on cancellation.
// It returns immediately when the runtime is inactive.
func WaitForShutdown(ctx context.Context) error {
	return globalRuntime.waitForShutdown(ctx)
}
