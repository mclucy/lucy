package style

import (
	"os"
	"sync"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"
)

var IsTerminal = term.IsTerminal(int(os.Stdout.Fd()))

var ensureTermColorsOnce sync.Once

// EnsureTermColors lazily initializes UserColors and ValidUserColors
// on first call, using sync.Once for thread safety.
func EnsureTermColors() {
	ensureTermColorsOnce.Do(getTermProfileColors)
}

func TermWidth() int {
	width, _, _ := term.GetSize(0)
	if width <= 0 {
		return 80
	}
	return width
}

func TermHeight() int {
	_, height, _ := term.GetSize(0)
	return height
}

func HasDarkBackground() bool {
	return lipgloss.HasDarkBackground(os.Stdin, os.Stdout)
}

func getTermProfileColors() {
	for i := ansi.BasicColor(0); i < 16; i++ {
		c := osc4Query(uint8(i))
		if c == nil {
			return
		}
		UserColors[i] = c
	}
	ValidUserColors = true
}
