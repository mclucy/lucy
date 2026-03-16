// Package progress provides a terminal progress bar backed by the charm stack
// (bubbletea + bubbles/progress + lipgloss).
//
// Unlike the parent tui package which is a one-shot static renderer, this
// package uses bubbletea for live, interactive progress display.
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
	"io"
)

// Tracker is a thread-safe progress bar controller.
//
// A Tracker is created with [NewTracker] and automatically starts displaying.
// External goroutines update progress via [Tracker.SetPercent],
// [Tracker.IncrPercent], and [Tracker.SetMessage].
// Call [Tracker.Close] to finish and exit the progress bar.
type Tracker struct {
	id entryID
}

// NewTracker creates a [Tracker] with the given title and starts displaying it.
func NewTracker(title string) *Tracker {
	id := globalRuntime.registerEntry(title)
	return &Tracker{id: id}
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

// Close completes the progress bar (jumps to 100 %) and exits the program.
func (t *Tracker) Close() {
	globalRuntime.send(t.id, closeMsg{})
}

// Complete is similar to Close but with visual feedback
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

// setBytesProgress is an internal method used by proxyReader to send
// byte-level progress updates to the model.
func (t *Tracker) setBytesProgress(read, total int64) {
	globalRuntime.send(t.id, bytesProgressMsg{read: read, total: total})
}
