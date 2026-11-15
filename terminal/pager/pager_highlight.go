package pager

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/mclucy/lucy/terminal/style"
)

// highlightMatches wraps every match in a visible style while preserving the
// document's existing styling. The selected match uses a stronger style so
// n and N have an anchor.
func highlightMatches(
	text string,
	matches []searchMatch,
	selected int,
	theme style.Theme,
) string {
	if len(matches) == 0 {
		return text
	}
	matchStyle, selectedStyle := matchHighlightStyles(theme)
	var output strings.Builder
	matchIndex := 0
	for lineIndex, line := range strings.Split(text, "\n") {
		if lineIndex > 0 {
			output.WriteByte('\n')
		}
		var ranges []lipgloss.Range
		for matchIndex < len(matches) && matches[matchIndex].line == lineIndex {
			rangeStyle := matchStyle
			if matchIndex == selected {
				rangeStyle = selectedStyle
			}
			ranges = append(ranges, lipgloss.NewRange(
				matches[matchIndex].column,
				matches[matchIndex].end,
				rangeStyle,
			))
			matchIndex++
		}
		output.WriteString(lipgloss.StyleRanges(line, ranges...))
	}
	return output.String()
}

// matchHighlightStyles keeps attributes when color is off and uses a yellow
// search block when the theme allows color.
func matchHighlightStyles(theme style.Theme) (lipgloss.Style, lipgloss.Style) {
	if theme.Colors() {
		match := lipgloss.NewStyle().
			Background(lipgloss.Yellow).
			Foreground(lipgloss.Black)
		return match, match.Bold(true).Underline(true)
	}
	return lipgloss.NewStyle().Reverse(true),
		lipgloss.NewStyle().Reverse(true).Bold(true).Underline(true)
}
