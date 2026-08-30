package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/mclucy/lucy/terminal/style"
)

const keyColPadding = 2

func renderKeyStyled(title string) string {
	return style.Key(title)
}

func renderKeyFixed(title string, width int) string {
	styled := style.Key(title)
	visualWidth := lipgloss.Width(styled)
	padding := max(width-visualWidth, keyColPadding)
	return styled + strings.Repeat(" ", padding)
}

func renderDim(text string) string {
	return style.Muted(text)
}

func renderAnnot(annotation string) string {
	return "  " + style.Muted(annotation)
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
