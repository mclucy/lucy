package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/mclucy/lucy/tui/style"
)

var keyColumnWidth int

const keyColPadding = 2

func renderKey(title string) string {
	styled := style.Bold(style.Magenta(title))
	visualWidth := lipgloss.Width(styled)
	padding := keyColumnWidth - visualWidth
	if padding < 2 {
		padding = 2
	}
	return styled + strings.Repeat(" ", padding)
}

func renderDim(text string) string {
	return style.Dim(text)
}

func renderAnnot(annotation string) string {
	return "  " + style.Dim(annotation)
}

func renderTab() string {
	return strings.Repeat(" ", keyColumnWidth)
}

func renderSeparator(length int, dim bool) string {
	if length > style.TermWidth() {
		length = style.TermWidth()
	}
	sep := lipgloss.NewStyle().
		Faint(dim).
		Render(strings.Repeat(borderChar, length))
	return sep + "\n"
}
