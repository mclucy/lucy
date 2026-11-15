package pager

import (
	"strings"

	"github.com/mclucy/lucy/terminal/style"
)

const (
	sgrReset           = "\x1b[0m"
	sgrMatch           = "\x1b[43;30m"
	sgrSelected        = "\x1b[45;37;1m"
	sgrNoColorMatch    = "\x1b[7m"
	sgrNoColorSelected = "\x1b[1;7m"
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
	matchOn, selectedOn := highlightCodes(theme)
	var output strings.Builder
	matchIndex := 0
	for lineIndex, line := range strings.Split(text, "\n") {
		if lineIndex > 0 {
			output.WriteByte('\n')
		}
		start := matchIndex
		for matchIndex < len(matches) && matches[matchIndex].line == lineIndex {
			matchIndex++
		}
		if start == matchIndex {
			output.WriteString(line)
			continue
		}
		output.WriteString(highlightLine(
			line,
			matches[start:matchIndex],
			selected-start,
			matchOn,
			selectedOn,
		))
	}
	return output.String()
}

// highlightCodes keeps attributes when color is off and uses distinct
// backgrounds for ordinary and selected matches when the theme allows color.
func highlightCodes(theme style.Theme) (match, selected string) {
	if theme.Colors() {
		return sgrMatch, sgrSelected
	}
	return sgrNoColorMatch, sgrNoColorSelected
}

// highlightLine inserts styles in one forward pass. It tracks active SGR
// sequences so the original styling resumes after each match.
func highlightLine(
	line string,
	matches []searchMatch,
	selected int,
	matchOn, selectedOn string,
) string {
	var output strings.Builder
	output.Grow(len(line) + len(matches)*8)
	active := make([]string, 0, 4)
	matchIndex := 0
	visible := 0
	inMatch := false
	closeMatch := func() {
		if !inMatch {
			return
		}
		output.WriteString(sgrReset)
		for _, sequence := range active {
			output.WriteString(sequence)
		}
		inMatch = false
	}
	for i := 0; i < len(line); {
		if line[i] == 0x1b {
			end := ansiSequenceEnd(line, i)
			sequence := line[i:end]
			output.WriteString(sequence)
			if isSGR(sequence) {
				updateActiveSGR(&active, sequence)
				if inMatch {
					output.WriteString(highlightPrefix(matchIndex, selected, matchOn, selectedOn))
				}
			}
			i = end
			continue
		}
		for !inMatch && matchIndex < len(matches) && visible == matches[matchIndex].startByte {
			output.WriteString(highlightPrefix(matchIndex, selected, matchOn, selectedOn))
			inMatch = true
		}
		output.WriteByte(line[i])
		visible++
		i++
		if inMatch && visible == matches[matchIndex].endByte {
			closeMatch()
			matchIndex++
		}
	}
	closeMatch()
	return output.String()
}

func highlightPrefix(matchIndex, selected int, matchOn, selectedOn string) string {
	if matchIndex == selected {
		return selectedOn
	}
	return matchOn
}

func updateActiveSGR(active *[]string, sequence string) {
	params := sequence[2 : len(sequence)-1]
	first, _, _ := strings.Cut(params, ";")
	if first == "" || first == "0" {
		*active = (*active)[:0]
	}
	*active = append(*active, sequence)
}

func isSGR(sequence string) bool {
	return len(sequence) >= 3 &&
		sequence[0] == 0x1b &&
		sequence[1] == '[' &&
		sequence[len(sequence)-1] == 'm'
}

// ansiSequenceEnd returns the end of the escape sequence starting at start.
func ansiSequenceEnd(s string, start int) int {
	if start+1 >= len(s) {
		return start + 1
	}
	switch s[start+1] {
	case '[':
		for i := start + 2; i < len(s); i++ {
			if s[i] >= 0x40 && s[i] <= 0x7e {
				return i + 1
			}
		}
		return len(s)
	case ']':
		for i := start + 2; i < len(s); i++ {
			if s[i] == 0x07 {
				return i + 1
			}
			if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
				return i + 2
			}
		}
		return len(s)
	default:
		return start + 2
	}
}
