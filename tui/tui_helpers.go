package tui

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/mclucy/lucy/tools"
)

// keyColumnWidth takes the longest key label in the current context and adds
// padding to ensure alignment of values in a two-column layout. It is set by
// while Flush() is rendering the current view.
var keyColumnWidth int

// keyColPadding is the fixed padding (in characters) added to the key column width
// to ensure minimum spacing between keys and values.
const keyColPadding = 2

// renderKey renders a styled key label with fixed-width padding for alignment.
func renderKey(title string) string {
	styled := tools.Bold(tools.Magenta(title))
	visualWidth := lipgloss.Width(styled)
	padding := keyColumnWidth - visualWidth
	if padding < 2 {
		padding = 2
	}
	return styled + strings.Repeat(" ", padding)
}

// renderDim renders text with a dimmed/faint style.
func renderDim(text string) string {
	return tools.Dim(text)
}

// renderAnnot renders an inline annotation (dimmed, with leading spacing).
func renderAnnot(annotation string) string {
	return "  " + tools.Dim(annotation)
}

// renderTab returns whitespace matching the key column width, used for
// continuation lines that need to align with the value column.
func renderTab() string {
	return strings.Repeat(" ", keyColumnWidth)
}

// renderSeparator returns a horizontal separator line.
//
// A zero-length separator is allowed and will not render anything. However,
// lengths longer than the terminal width will be truncated to fit.
func renderSeparator(length int, dim bool) string {
	if length > tools.TermWidth() {
		length = tools.TermWidth()
	}
	sep := strings.Repeat("-", length)
	if dim {
		return renderDim(sep) + "\n"
	}
	return sep + "\n"
}
