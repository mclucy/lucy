package tui

// StatusLayoutMode describes how the status view arranges the logo and info
// blocks within the available terminal width.
type StatusLayoutMode int

const (
	// LayoutLargeLogoSideBySide places the large logo to the left of the
	// info block with a gap between them.
	LayoutLargeLogoSideBySide StatusLayoutMode = iota
	// LayoutSmallLogoSideBySide places the small logo to the left of the
	// info block with a gap between them.
	LayoutSmallLogoSideBySide
	// LayoutVertical stacks the logo above the info block (no
	// side-by-side).
	LayoutVertical
	// LayoutClipped renders only the info block, clipped to the terminal
	// width. Used when the terminal is narrower than minInfoWidth.
	LayoutClipped
	// LayoutInfoOnly renders only the info block with no logo at all.
	// Used in non-TTY (piped) contexts.
	LayoutInfoOnly
)

const (
	// statusLayoutGapWidth is the number of blank columns between the logo
	// and info blocks in side-by-side modes.
	statusLayoutGapWidth = 3
	// statusLayoutMinInfoWidth is the minimum number of columns the info
	// block needs to be useful.
	statusLayoutMinInfoWidth = 40
)

// StatusLayoutParams holds the result of layout negotiation: the chosen mode
// and the width budgets for each visual element.
type StatusLayoutParams struct {
	Mode      StatusLayoutMode
	LogoWidth int
	InfoWidth int
	GapWidth  int
}

// NegotiateStatusLayout decides which layout mode to use given the terminal
// width, the widths of the two logo variants, and whether the output is a
// TTY. It returns the mode together with pixel-budget details so that the
// compositor can render without further arithmetic.
func NegotiateStatusLayout(termWidth int, logoLargeWidth int, logoSmallWidth int, isTTY bool) StatusLayoutParams {
	if !isTTY {
		return StatusLayoutParams{
			Mode:      LayoutInfoOnly,
			LogoWidth: 0,
			InfoWidth: termWidth,
			GapWidth:  0,
		}
	}

	// Large logo side-by-side: logo + gap + info (>= minInfoWidth).
	if termWidth >= logoLargeWidth+statusLayoutGapWidth+statusLayoutMinInfoWidth {
		return StatusLayoutParams{
			Mode:      LayoutLargeLogoSideBySide,
			LogoWidth: logoLargeWidth,
			InfoWidth: termWidth - logoLargeWidth - statusLayoutGapWidth,
			GapWidth:  statusLayoutGapWidth,
		}
	}

	// Small logo side-by-side.
	if termWidth >= logoSmallWidth+statusLayoutGapWidth+statusLayoutMinInfoWidth {
		return StatusLayoutParams{
			Mode:      LayoutSmallLogoSideBySide,
			LogoWidth: logoSmallWidth,
			InfoWidth: termWidth - logoSmallWidth - statusLayoutGapWidth,
			GapWidth:  statusLayoutGapWidth,
		}
	}

	// Vertical: logo stacked above info. Requires at least minInfoWidth.
	if termWidth >= statusLayoutMinInfoWidth {
		return StatusLayoutParams{
			Mode:      LayoutVertical,
			LogoWidth: logoLargeWidth,
			InfoWidth: termWidth,
			GapWidth:  0,
		}
	}

	// Clipped: terminal too narrow even for minInfoWidth.
	return StatusLayoutParams{
		Mode:      LayoutClipped,
		LogoWidth: 0,
		InfoWidth: termWidth,
		GapWidth:  0,
	}
}
