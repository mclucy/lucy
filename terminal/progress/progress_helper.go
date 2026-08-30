package progress

import (
	"math"

	"github.com/mclucy/lucy/internal/fn"
	"github.com/mclucy/lucy/terminal/style"
)

func clamp01(v float64) float64 {
	return math.Max(0, math.Min(1, v))
}

func getTrackerWidth(termWidth int) (w int) {
	if termWidth <= 0 {
		// unset or invalid width
		termWidth = style.TermWidth()
	}
	w = fn.Ternary(termWidth >= 125, 100, termWidth-50)
	w = fn.Ternary(w < 10, 10, w)
	return w
}
