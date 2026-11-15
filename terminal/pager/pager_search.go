package pager

import (
	"fmt"
	"strings"
	"unicode"

	"charm.land/bubbles/v2/viewport"
	"github.com/charmbracelet/x/ansi"
)

// searchState holds one pager's incremental search.
type searchState struct {
	active  bool
	query   string
	term    string
	matches []searchMatch
	index   int
}

// searchMatch locates one occurrence by content line and display columns, and
// records the wrapped screen row at the width used for the search.
type searchMatch struct {
	line      int
	column    int
	end       int
	startByte int
	endByte   int
	row       int
}

func (s *searchState) begin() {
	s.active = true
	s.query = s.term
}

func (s *searchState) cancel() {
	s.active = false
}

func (s *searchState) typeText(text string) {
	if text == "" {
		return
	}
	s.query += text
}

// paste accepts one pasted line; an embedded newline marks a multi-line
// paste, which would not fit a single-line search prompt.
func (s *searchState) paste(text string) {
	text = strings.TrimRight(text, "\r\n")
	if text == "" || strings.ContainsAny(text, "\r\n") {
		return
	}
	s.typeText(text)
}

func (s *searchState) backspace() {
	runes := []rune(s.query)
	if len(runes) == 0 {
		return
	}
	s.query = string(runes[:len(runes)-1])
}

// submit scans the document and moves to the first match at or below the
// current top line. Matches stay available for n and N.
func (s *searchState) submit(target *viewport.Model, text string, width int) {
	s.active = false
	s.term = strings.TrimSpace(s.query)
	s.matches = nil
	s.index = 0
	if s.term == "" {
		return
	}
	caseSensitive := hasUpper(s.term)
	lineNumber := 0
	row := 0
	for line := range strings.SplitSeq(text, "\n") {
		for _, span := range matchSpans(line, s.term, caseSensitive) {
			s.matches = append(s.matches, searchMatch{
				line:      lineNumber,
				column:    span.start,
				end:       span.end,
				startByte: span.startByte,
				endByte:   span.endByte,
				row:       row + span.start/max(1, width),
			})
		}
		row += wrappedRows(line, width)
		lineNumber++
	}
	if len(s.matches) == 0 {
		return
	}
	top := target.YOffset()
	for i, match := range s.matches {
		if match.row >= top {
			s.index = i
			break
		}
	}
}

func (s *searchState) next(step int) {
	if len(s.matches) == 0 {
		return
	}
	s.index = (s.index + step + len(s.matches)) % len(s.matches)
}

// matchSpan is one occurrence's display-column and visible-byte range.
type matchSpan struct {
	start     int
	end       int
	startByte int
	endByte   int
}

// matchSpans returns every non-overlapping occurrence in one line, ignoring
// escape sequences in the visible text. Widths accumulate across disjoint
// segments so each line is measured once.
func matchSpans(line, term string, caseSensitive bool) []matchSpan {
	haystack := ansi.Strip(line)
	needle := term
	if !caseSensitive {
		haystack = strings.ToLower(haystack)
		needle = strings.ToLower(needle)
	}
	if needle == "" {
		return nil
	}
	var spans []matchSpan
	lastByte, lastColumn := 0, 0
	for offset := 0; offset < len(haystack); {
		index := strings.Index(haystack[offset:], needle)
		if index < 0 {
			break
		}
		start := offset + index
		end := start + len(needle)
		lastColumn += ansi.StringWidth(haystack[lastByte:start])
		startColumn := lastColumn
		lastColumn += ansi.StringWidth(haystack[start:end])
		spans = append(spans, matchSpan{
			start:     startColumn,
			end:       lastColumn,
			startByte: start,
			endByte:   end,
		})
		lastByte = end
		offset = end
	}
	return spans
}

// showMatch scrolls to the wrapped row that holds the match.
func showMatch(target *viewport.Model, match searchMatch) {
	target.EnsureVisible(match.row, 0, 0)
}

// wrappedRows counts the screen rows one content line occupies.
func wrappedRows(line string, width int) int {
	width = max(1, width)
	return max(1, (ansi.StringWidth(line)+width-1)/width)
}

// rewrap updates stored rows after the terminal width changes.
func (s *searchState) rewrap(text string, width int) {
	if len(s.matches) == 0 {
		return
	}
	row := 0
	lineNumber := 0
	matchIndex := 0
	for line := range strings.SplitSeq(text, "\n") {
		for matchIndex < len(s.matches) && s.matches[matchIndex].line == lineNumber {
			s.matches[matchIndex].row = row + s.matches[matchIndex].column/max(1, width)
			matchIndex++
		}
		row += wrappedRows(line, width)
		lineNumber++
	}
}

func (s *searchState) label() string {
	if len(s.matches) == 0 {
		return fmt.Sprintf("/%s (no matches)", s.term)
	}
	return fmt.Sprintf("/%s (%d/%d)", s.term, s.index+1, len(s.matches))
}

func hasUpper(text string) bool {
	return strings.ContainsFunc(text, unicode.IsUpper)
}
