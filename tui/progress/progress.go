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
//	if err := t.Run(); err != nil { ... }
package progress

import (
	"errors"
	"io"
	"os"

	"charm.land/bubbles/v2/progress"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Tracker is a thread-safe progress bar controller.
//
// A Tracker is created with [NewTracker] and started with [Tracker.Run].
// External goroutines update progress via [Tracker.SetPercent],
// [Tracker.IncrPercent], and [Tracker.SetMessage].
// Call [Tracker.Close] to finish and exit the progress bar.
type Tracker struct {
	title   string
	program *tea.Program
}

// NewTracker creates a [Tracker] with the given title.
// Call [Tracker.Run] to display it.
func NewTracker(title string) *Tracker {
	return &Tracker{title: title}
}

// Run starts the progress bar and blocks until [Tracker.Close] is called
// or the user presses Ctrl+C.
func (t *Tracker) Run() error {
	bar := progress.New(
		progress.WithColors(lipgloss.Magenta, lipgloss.BrightMagenta),
	)

	m := model{
		bar:   bar,
		title: t.title,
	}

	t.program = tea.NewProgram(m)
	_, err := t.program.Run()
	if errors.Is(err, tea.ErrInterrupted) {
		os.Exit(130)
	}
	return err
}

// SetPercent sets the current progress to p (clamped to [0, 1]).
func (t *Tracker) SetPercent(p float64) {
	if t.program != nil {
		t.program.Send(setPercentMsg(clamp01(p)))
	}
}

// IncrPercent adds delta to the current progress.
func (t *Tracker) IncrPercent(delta float64) {
	if t.program != nil {
		t.program.Send(incrPercentMsg(delta))
	}
}

// SetMessage updates the status text shown alongside the bar.
func (t *Tracker) SetMessage(msg string) {
	if t.program != nil {
		t.program.Send(setMessageMsg(msg))
	}
}

// Close completes the progress bar (jumps to 100 %) and exits the program.
func (t *Tracker) Close() {
	if t.program != nil {
		t.program.Send(closeMsg{})
	}
}

// Complete is similar to Close but with visual feedback
func (t *Tracker) Complete(msg string) {
	if t.program != nil {
		t.program.Send(completeMsg(msg))
	}
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
	if t.program != nil {
		t.program.Send(bytesProgressMsg{read: read, total: total})
	}
}
