package style

import (
	"io"
	"os"
	"sync"

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
	Width      int
	Height     int
	IsTTY      bool
	Dark       bool
	background func() bool
}

// DetectTerminals detects geometry immediately and queries the background only
// when colored Markdown needs it. Streams on the same terminal share a query.
func DetectTerminals(input io.Reader, stdout, stderr io.Writer) (Terminal, Terminal) {
	out := detectTerminal(input, stdout)
	errOut := detectTerminal(input, stderr)
	if out.background != nil && errOut.background != nil {
		outFile := stdout.(*os.File)
		errFile := stderr.(*os.File)
		outInfo, outErr := outFile.Stat()
		errInfo, errErr := errFile.Stat()
		if outErr == nil && errErr == nil && os.SameFile(outInfo, errInfo) {
			errOut.background = out.background
		}
	}
	return out, errOut
}

func detectTerminal(input io.Reader, w io.Writer) Terminal {
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
		detected.background = sync.OnceValue(func() bool {
			return lipgloss.HasDarkBackground(terminalInput, file)
		})
	}
	return detected
}
