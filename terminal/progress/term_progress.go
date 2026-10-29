// Package progress presents command progress on stderr.
//
// A renderer is safe for concurrent use. Long phases announce themselves with
// PhaseStarted and PhaseDone, transient work with Status and Download, and
// permanent findings with Println, which keeps them above the animated area.
package progress

import (
	"io"
	"time"

	"github.com/mclucy/lucy/terminal/style"
)

// Options carries the presentation state a renderer needs. The caller owns
// every value: a renderer never reads process streams or global styles.
type Options struct {
	// Writer receives every permanent line and, when animated, the live area.
	Writer io.Writer
	// Theme styles progress text.
	Theme style.Theme
	// Terminal describes the capabilities of Writer.
	Terminal style.Terminal
	// Interactive allows an animated area when Terminal is a terminal.
	Interactive bool
}

// Renderer presents one command's progress.
type Renderer interface {
	// PhaseStarted announces a long-running stage.
	PhaseStarted(text string)
	// PhaseDone reports that the stage ended and prints it permanently.
	PhaseDone(text string, elapsed time.Duration)
	// Status replaces the live status line. Non-interactive renderers print it
	// at most once per heartbeat interval.
	Status(text string)
	// Download reports byte progress for the artifact being transferred.
	Download(name string, read, total int64)
	// DownloadDone clears the artifact progress.
	DownloadDone()
	// Println writes permanent lines above the live area.
	Println(lines ...string)
	// LogWriter returns a writer whose complete lines appear in the live area.
	// A subprocess uses it to stream its output without owning the terminal.
	LogWriter() io.Writer
	// Close stops the renderer. It is safe to call more than once.
	Close()
}

// New returns the renderer that suits options: an animated area only when the
// caller allows interactivity and Writer is a terminal, append-only lines
// otherwise. A nil Writer yields Discard, which suits machine-readable
// commands that must keep stderr free of progress.
func New(options Options) Renderer {
	if options.Writer == nil {
		return Discard()
	}
	if options.Interactive && options.Terminal.IsTTY {
		return newLive(options)
	}
	return newPlain(options.Writer, options.Theme)
}

// Discard drops every update. It suits machine-readable commands, which must
// keep stderr free of progress.
func Discard() Renderer { return discardRenderer{} }

type discardRenderer struct{}

func (discardRenderer) PhaseStarted(string)             {}
func (discardRenderer) PhaseDone(string, time.Duration) {}
func (discardRenderer) Status(string)                   {}
func (discardRenderer) Download(string, int64, int64)   {}
func (discardRenderer) DownloadDone()                   {}
func (discardRenderer) Println(...string)               {}
func (discardRenderer) LogWriter() io.Writer            { return io.Discard }
func (discardRenderer) Close()                          {}
