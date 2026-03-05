package progress

import (
	"math"

	"github.com/mclucy/lucy/tools"
)

func clamp01(v float64) float64 {
	return math.Max(0, math.Min(1, v))
}

func defaultBarWidth(termWidth int) (w int) {
	if termWidth <= 0 {
		// unset or invalid width
		termWidth = tools.TermWidth()
	}
	w = tools.Ternary(termWidth >= 125, 100, termWidth-50)
	w = tools.Ternary(w < 10, 10, w)
	return w
}
