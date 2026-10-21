// Package progress presents command progress on stderr.
//
// A renderer is safe for concurrent use. Long phases announce themselves with
// PhaseStarted and PhaseDone, transient work with Status and Download, and
// permanent findings with Println, which keeps them above the animated area.
package progress

import (
	"io"
	"os"
	"time"

	"github.com/mclucy/lucy/terminal/style"
	"golang.org/x/term"
)

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

// New returns the renderer that suits w. Animated output requires w to be a
// terminal and interactive mode to be enabled; anything else receives
// append-only lines.
func New(w io.Writer) Renderer {
	if file, ok := w.(*os.File); ok &&
		style.InteractiveEnabled() &&
		term.IsTerminal(int(file.Fd())) {
		return newLive(w)
	}
	return newPlain(w)
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
