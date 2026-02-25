package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/mclucy/lucy/tools"
)

// keyColumnWidth is the fixed width for the key column in key-value output.
// This replaces the dynamic tab writer alignment with a predictable layout.
const keyColumnWidth = 16

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

// renderSeparator returns a horizontal separator line. A length of 0 produces
// a line spanning 75% of the terminal width.
func renderSeparator(length int, dim bool) string {
	if length == 0 {
		length = tools.TermWidth() * 3 / 4
	} else if length > tools.TermWidth() {
		length = tools.TermWidth()
	}
	sep := strings.Repeat("-", length)
	if dim {
		return renderDim(sep) + "\n"
	}
	return sep + "\n"
}
