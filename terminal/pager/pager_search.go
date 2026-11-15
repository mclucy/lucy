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
	matches []int
	index   int
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
	needle := s.term
	if !hasUpper(s.term) {
		needle = strings.ToLower(needle)
	}
	lineNumber := 0
	for line := range strings.SplitSeq(text, "\n") {
		haystack := ansi.Strip(line)
		if !hasUpper(s.term) {
			haystack = strings.ToLower(haystack)
		}
		if strings.Contains(haystack, needle) {
			s.matches = append(s.matches, lineNumber)
		}
		lineNumber++
	}
	if len(s.matches) == 0 {
		return
	}
	top := target.YOffset()
	for i, line := range s.matches {
		if screenLine(text, line, width) >= top {
			s.index = i
			break
		}
	}
	showLine(target, text, s.matches[s.index], width)
}

func (s *searchState) next(target *viewport.Model, text string, width, step int) {
	if len(s.matches) == 0 {
		return
	}
	s.index = (s.index + step + len(s.matches)) % len(s.matches)
	showLine(target, text, s.matches[s.index], width)
}

// showLine scrolls to a content line, converting it to the wrapped screen
// line index that the viewport's offset uses.
func showLine(target *viewport.Model, text string, line, width int) {
	target.EnsureVisible(screenLine(text, line, width), 0, 0)
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
