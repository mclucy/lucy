package style

import (
	"os"
	"sync"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"
)

// The two streams answer different questions: data output may be piped while
// diagnostics still reach a terminal, and the reverse. Presentation that
// renders to a stream must consult that stream's terminal state.
var (
	StdoutIsTerminal = term.IsTerminal(int(os.Stdout.Fd()))
	StderrIsTerminal = term.IsTerminal(int(os.Stderr.Fd()))
)

var ensureTermColorsOnce = sync.OnceFunc(getTermProfileColors)

// EnsureTermColors lazily initializes UserColors and ValidUserColors
// exactly once.
func EnsureTermColors() {
	ensureTermColorsOnce()
}

// TermWidth reports the terminal width for data rendered on stdout.
func TermWidth() int {
	return streamWidth(int(os.Stdout.Fd()))
}

// StderrWidth reports the terminal width for progress and diagnostics.
func StderrWidth() int {
	return streamWidth(int(os.Stderr.Fd()))
}

func streamWidth(fd int) int {
	width, _, _ := term.GetSize(fd)
	if width <= 0 {
		return 80
	}
	return width
}

func TermHeight() int {
	_, height, _ := term.GetSize(int(os.Stdout.Fd()))
	if height <= 0 {
		return 24
	}
	return height
}

func HasDarkBackground() bool {
	return lipgloss.HasDarkBackground(os.Stdin, os.Stdout)
}

func getTermProfileColors() {
	for i := range ansi.BasicColor(16) {
		c := osc4Query(uint8(i))
		if c == nil {
			return
		}
		UserColors[i] = c
	}
	ValidUserColors = true
}
