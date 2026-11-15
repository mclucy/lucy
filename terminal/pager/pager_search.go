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

// searchMatch locates one occurrence by content line and display column.
type searchMatch struct {
	line   int
	column int
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
	for line := range strings.SplitSeq(text, "\n") {
		for _, column := range matchColumns(line, s.term, caseSensitive) {
			s.matches = append(s.matches, searchMatch{line: lineNumber, column: column})
		}
		lineNumber++
	}
	if len(s.matches) == 0 {
		return
	}
	top := target.YOffset()
	for i, match := range s.matches {
		if screenRow(text, match, width) >= top {
			s.index = i
			break
		}
	}
	showMatch(target, text, s.matches[s.index], width)
}

func (s *searchState) next(target *viewport.Model, text string, width, step int) {
	if len(s.matches) == 0 {
		return
	}
	s.index = (s.index + step + len(s.matches)) % len(s.matches)
	showMatch(target, text, s.matches[s.index], width)
}

// matchColumns returns the display column of every non-overlapping
// occurrence in one line, ignoring escape sequences in the visible text.
func matchColumns(line, term string, caseSensitive bool) []int {
	haystack := ansi.Strip(line)
	needle := term
	if !caseSensitive {
		haystack = strings.ToLower(haystack)
		needle = strings.ToLower(needle)
	}
	if needle == "" {
		return nil
	}
	var columns []int
	for offset := 0; offset < len(haystack); {
		index := strings.Index(haystack[offset:], needle)
		if index < 0 {
			break
		}
		start := offset + index
		columns = append(columns, ansi.StringWidth(haystack[:start]))
		offset = start + len(needle)
	}
	return columns
}

// showMatch scrolls to the wrapped row that holds the match.
func showMatch(target *viewport.Model, text string, match searchMatch, width int) {
	target.EnsureVisible(screenRow(text, match, width), 0, 0)
}

// screenRow converts a content line and column to a wrapped screen row.
func screenRow(text string, match searchMatch, width int) int {
	return screenLine(text, match.line, width) + match.column/max(1, width)
}

// screenLine counts wrapped rows before line at the given content width.
func screenLine(text string, line, width int) int {
	width = max(1, width)
	offset := 0
	current := 0
	for content := range strings.SplitSeq(text, "\n") {
		if current == line {
			return offset
		}
		offset += max(1, (ansi.StringWidth(content)+width-1)/width)
		current++
	}
	return offset
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
