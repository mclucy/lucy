package style

import (
	"io"
	"os"

	"charm.land/lipgloss/v2"
	"golang.org/x/term"
)

// Conventional geometry when the stream cannot report its own size.
const (
	DefaultTerminalWidth  = 80
	DefaultTerminalHeight = 24
)

// Terminal describes the capabilities of one output stream. Presentation that
// renders to a stream derives its geometry and terminal state from that
// stream alone, never from a package-level snapshot.
type Terminal struct {
	Width  int
	Height int
	IsTTY  bool
	Dark   bool
}

// DetectTerminal derives the capabilities of w. Non-terminal output uses
// conventional geometry. Background queries use only the supplied terminal
// input; nil input leaves the dark-background default without reading anything.
func DetectTerminal(input io.Reader, w io.Writer) Terminal {
	detected := Terminal{
		Width:  DefaultTerminalWidth,
		Height: DefaultTerminalHeight,
		Dark:   true,
	}
	file, ok := w.(*os.File)
	if !ok || !term.IsTerminal(int(file.Fd())) {
		return detected
	}
	detected.IsTTY = true
	if width, height, err := term.GetSize(int(file.Fd())); err == nil {
		if width > 0 {
			detected.Width = width
		}
		if height > 0 {
			detected.Height = height
		}
	}
	if terminalInput, ok := input.(*os.File); ok && term.IsTerminal(int(terminalInput.Fd())) {
		detected.Dark = lipgloss.HasDarkBackground(terminalInput, file)
	}
	return detected
}
