package pager

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"charm.land/bubbles/v2/viewport"
	"charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mclucy/lucy/terminal/style"
)

func TestNeedsPager(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		mode           Mode
		text           string
		width          int
		terminalHeight int
		want           bool
	}{
		{name: "never", mode: Never, text: strings.Repeat("line\n", 100), width: 80, terminalHeight: 10},
		{name: "empty", mode: Always, text: "", width: 80, terminalHeight: 10},
		{name: "always short", mode: Always, text: "one line", width: 80, terminalHeight: 24, want: true},
		{name: "auto fits exactly", mode: Auto, text: strings.Repeat("line\n", 8), width: 80, terminalHeight: 10},
		{name: "auto overflows", mode: Auto, text: strings.Repeat("line\n", 9), width: 80, terminalHeight: 10, want: true},
		{name: "auto tall terminal", mode: Auto, text: strings.Repeat("line\n", 40), width: 80, terminalHeight: 80},
		{
			name:           "auto wraps long lines",
			mode:           Auto,
			text:           strings.Repeat(strings.Repeat("x", 100)+"\n", 10),
			width:          40,
			terminalHeight: 24,
			want:           true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := needsPager(test.mode, test.text, test.width, test.terminalHeight); got != test.want {
				t.Fatalf("needsPager() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestParseMode(t *testing.T) {
	t.Parallel()

	for input, want := range map[string]Mode{
		"auto":   Auto,
		"always": Always,
		"never":  Never,
	} {
		got, err := ParseMode(input)
		if err != nil {
			t.Fatalf("ParseMode(%q): %v", input, err)
		}
		if got != want {
			t.Fatalf("ParseMode(%q) = %v, want %v", input, got, want)
		}
	}
	if _, err := ParseMode("sometimes"); err == nil {
		t.Fatal("ParseMode accepted an unknown mode")
	}
}

func TestModelNavigationAndSearch(t *testing.T) {
	t.Parallel()

	lines := make([]string, 100)
	for index := range lines {
		lines[index] = "content line"
	}
	lines[60] = "the needle is here"
	lines[75] = "another needle"
	m := newModel(Options{
		Title:    "lucy test",
		Text:     strings.Join(lines, "\n"),
		Terminal: style.Terminal{Width: 40, Height: 10},
		Theme:    style.New(false),
	})
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 10})

	press := func(text string, code rune) {
		t.Helper()
		m.Update(tea.KeyPressMsg{Text: text, Code: code})
	}

	press("G", 'G')
	if !m.viewport.AtBottom() {
		t.Fatal("G did not reach the bottom")
	}
	press("g", 'g')
	if !m.viewport.AtTop() {
		t.Fatal("g did not reach the top")
	}

	press("/", '/')
	for _, letter := range "needle" {
		press(string(letter), letter)
	}
	press("", tea.KeyEnter)
	if len(m.search.matches) != 2 {
		t.Fatalf("search matched %d lines, want 2", len(m.search.matches))
	}
	if m.search.matches[0].line != 60 || m.search.matches[1].line != 75 {
		t.Fatalf("search matches = %v, want lines [60 75]", m.search.matches)
	}
	if !lineVisible(&m.viewport, 60) {
		t.Fatalf("first match is outside the viewport at offset %d", m.viewport.YOffset())
	}

	press("n", 'n')
	if m.search.index != 1 || !lineVisible(&m.viewport, 75) {
		t.Fatalf("n did not move to the second match (index %d)", m.search.index)
	}
	press("n", 'n')
	if m.search.index != 0 || !lineVisible(&m.viewport, 60) {
		t.Fatalf("n did not wrap to the first match (index %d)", m.search.index)
	}
}

func TestSearchSkipsWrappedRows(t *testing.T) {
	t.Parallel()

	text := strings.Repeat(strings.Repeat("x", 100)+"\n", 20) + "the needle\n" + strings.Repeat("tail\n", 30)
	m := newModel(Options{
		Text:     text,
		Terminal: style.Terminal{Width: 40, Height: 10},
		Theme:    style.New(false),
	})
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	m.search.begin()
	for _, letter := range "needle" {
		m.handleSearchKey(tea.KeyPressMsg{Text: string(letter), Code: letter})
	}
	m.handleSearchKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(m.search.matches) != 1 || m.search.matches[0].line != 20 {
		t.Fatalf("matches = %v, want line 20", m.search.matches)
	}
	row := m.search.matches[0].row
	if row != 60 {
		t.Fatalf("match row = %d, want 60", row)
	}
}

func TestSearchLocatesMatchInsideWrappedLine(t *testing.T) {
	t.Parallel()

	text := strings.Repeat("x", 800) + "needle" + strings.Repeat("\ntail", 20)
	m := newModel(Options{
		Text:     text,
		Terminal: style.Terminal{Width: 40, Height: 10},
		Theme:    style.New(false),
	})
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	m.search.begin()
	for _, letter := range "needle" {
		m.handleSearchKey(tea.KeyPressMsg{Text: string(letter), Code: letter})
	}
	m.handleSearchKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(m.search.matches) != 1 || m.search.matches[0].column != 800 {
		t.Fatalf("matches = %v, want column 800", m.search.matches)
	}
	row := m.search.matches[0].row
	if row != 20 {
		t.Fatalf("match row = %d, want 20", row)
	}
}

func TestSearchFindsEveryOccurrenceOnOneLine(t *testing.T) {
	t.Parallel()

	m := newModel(Options{
		Text:     "needle and needle and needle",
		Terminal: style.Terminal{Width: 40, Height: 5},
		Theme:    style.New(false),
	})
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 5})
	m.search.begin()
	for _, letter := range "needle" {
		m.handleSearchKey(tea.KeyPressMsg{Text: string(letter), Code: letter})
	}
	m.handleSearchKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(m.search.matches) != 3 {
		t.Fatalf("matched %d occurrences, want 3", len(m.search.matches))
	}
	for i := 1; i < len(m.search.matches); i++ {
		if m.search.matches[i].column <= m.search.matches[i-1].column {
			t.Fatalf("columns are not increasing: %v", m.search.matches)
		}
	}
}

func TestResizeRewrapsSearchMatches(t *testing.T) {
	t.Parallel()

	text := strings.Repeat(strings.Repeat("x", 100)+"\n", 20) + "needle"
	m := newModel(Options{
		Text:     text,
		Terminal: style.Terminal{Width: 40, Height: 10},
		Theme:    style.New(false),
	})
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	m.search.begin()
	for _, letter := range "needle" {
		m.handleSearchKey(tea.KeyPressMsg{Text: string(letter), Code: letter})
	}
	m.handleSearchKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(m.search.matches) != 1 || m.search.matches[0].row != 60 {
		t.Fatalf("match row = %v, want 60", m.search.matches)
	}
	m.resize(120, 10)
	if m.search.matches[0].row != 20 {
		t.Fatalf("rewrapped row = %d, want 20", m.search.matches[0].row)
	}
}

func TestSearchAcceptsSingleLinePaste(t *testing.T) {
	t.Parallel()

	m := newModel(Options{
		Text:     "a needle here",
		Terminal: style.Terminal{Width: 40, Height: 5},
		Theme:    style.New(false),
	})
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 5})
	m.search.begin()
	m.Update(tea.PasteMsg{Content: "needle\r\n"})
	if m.search.query != "needle" {
		t.Fatalf("query = %q, want needle", m.search.query)
	}
	m.Update(tea.PasteMsg{Content: "two\nlines"})
	if m.search.query != "needle" {
		t.Fatalf("multi-line paste changed query to %q", m.search.query)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(m.search.matches) != 1 {
		t.Fatalf("pasted search matched %d lines, want 1", len(m.search.matches))
	}
}

func TestSearchHighlightsMatches(t *testing.T) {
	t.Parallel()

	m := newModel(Options{
		Text:     "a needle here and needle there",
		Terminal: style.Terminal{Width: 60, Height: 5},
		Theme:    style.New(false),
	})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 5})
	m.search.begin()
	for _, letter := range "needle" {
		m.handleSearchKey(tea.KeyPressMsg{Text: string(letter), Code: letter})
	}
	m.handleSearchKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	content := m.viewport.GetContent()
	if ansi.Strip(content) != m.text {
		t.Fatalf("highlighting changed visible content: %q", ansi.Strip(content))
	}
	if content == m.text {
		t.Fatal("search left the content unhighlighted")
	}
	if len(m.search.matches) != 2 {
		t.Fatalf("matched %d occurrences, want 2", len(m.search.matches))
	}
	first := m.search.matches[0]
	if first.end-first.column != 6 {
		t.Fatalf("match width = %d, want 6", first.end-first.column)
	}
}

func TestMatchSpansTrackOffsets(t *testing.T) {
	t.Parallel()

	spans := matchSpans("\x1b[2m界needle\x1b[0m and needle", "needle", true)
	if len(spans) != 2 {
		t.Fatalf("spans = %d, want 2", len(spans))
	}
	first := spans[0]
	if first.startByte != len("界") || first.endByte != len("界needle") {
		t.Fatalf("first bytes = %d..%d", first.startByte, first.endByte)
	}
	if first.start != 2 || first.end != 8 {
		t.Fatalf("first columns = %d..%d, want 2..8", first.start, first.end)
	}
}

func TestMatchSpansMapLoweredOffsets(t *testing.T) {
	t.Parallel()

	line := "İneedle"
	spans := matchSpans(line, "needle", false)
	if len(spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(spans))
	}
	match := spans[0]
	if match.startByte != len("İ") || match.endByte != len(line) {
		t.Fatalf("bytes = %d..%d, want %d..%d", match.startByte, match.endByte, len("İ"), len(line))
	}
	if match.start != 1 || match.end != 7 {
		t.Fatalf("columns = %d..%d, want 1..7", match.start, match.end)
	}
	highlighted := highlightMatches(
		line,
		[]searchMatch{{
			line:      0,
			column:    match.start,
			end:       match.end,
			startByte: match.startByte,
			endByte:   match.endByte,
		}},
		0,
		style.New(false),
	)
	if !utf8.ValidString(highlighted) {
		t.Fatalf("highlight produced invalid UTF-8: %q", highlighted)
	}
	if ansi.Strip(highlighted) != line {
		t.Fatalf("visible text = %q, want %q", ansi.Strip(highlighted), line)
	}
}

func TestHighlightDenseMatchesPreservesText(t *testing.T) {
	t.Parallel()

	line := strings.Repeat("x", 200)
	spans := matchSpans(line, "x", true)
	matches := make([]searchMatch, len(spans))
	for i, span := range spans {
		matches[i] = searchMatch{
			column:    span.start,
			end:       span.end,
			startByte: span.startByte,
			endByte:   span.endByte,
		}
	}
	highlighted := highlightMatches(line, matches, 0, style.New(false))
	if ansi.Strip(highlighted) != line {
		t.Fatalf("dense highlighting changed visible text")
	}
	if len(matches) != len(line) {
		t.Fatalf("spans = %d, want %d", len(matches), len(line))
	}
}

func TestHighlightMatchesPreservesStyles(t *testing.T) {
	t.Parallel()

	text := "\x1b[2mneedle still dim\x1b[0m"
	matches := []searchMatch{{line: 0, column: 0, end: 6}}
	highlighted := highlightMatches(text, matches, 0, style.New(false))
	if ansi.Strip(highlighted) != ansi.Strip(text) {
		t.Fatalf("visible text changed: %q", ansi.Strip(highlighted))
	}
	if !strings.Contains(highlighted, "\x1b[2m") {
		t.Fatalf("existing dim style was lost: %q", highlighted)
	}
	other := highlightMatches(text, matches, 1, style.New(false))
	if highlighted == other {
		t.Fatal("selected match does not use a distinct style")
	}
}

func TestEscapeDoesNotQuit(t *testing.T) {
	t.Parallel()

	m := newModel(Options{
		Text:     "content",
		Terminal: style.Terminal{Width: 40, Height: 10},
		Theme:    style.New(false),
	})
	command := m.handleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if command != nil {
		t.Fatal("escape returned a quit command")
	}
	if m.hint != "press q to quit" {
		t.Fatalf("hint = %q, want press q to quit", m.hint)
	}
	if !strings.Contains(ansi.Strip(m.footer()), "press q to quit") {
		t.Fatalf("footer lacks quit hint: %q", ansi.Strip(m.footer()))
	}

	command = m.handleKey(tea.KeyPressMsg{Text: "q", Code: 'q'})
	if command == nil {
		t.Fatal("q returned no command")
	}
	if _, ok := command().(tea.QuitMsg); !ok {
		t.Fatalf("q produced %T, want tea.QuitMsg", command())
	}
}

func TestEscapeCancelsSearch(t *testing.T) {
	t.Parallel()

	m := newModel(Options{
		Text:     "needle",
		Terminal: style.Terminal{Width: 40, Height: 10},
		Theme:    style.New(false),
	})
	m.search.begin()
	m.search.typeText("needle")
	command := m.handleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if command != nil {
		t.Fatal("escape returned a quit command while searching")
	}
	if m.search.active {
		t.Fatal("escape did not cancel the search prompt")
	}
	if m.hint != "" {
		t.Fatalf("canceled search left hint %q", m.hint)
	}
}

func TestFitLineKeepsWidth(t *testing.T) {
	t.Parallel()

	theme := style.New(true)
	for _, width := range []int{1, 10, 40, 120} {
		line := fitLine(theme.Accent("lucy docs read"), theme.Muted("42% q quit"), width)
		if got := ansi.StringWidth(line); got > width {
			t.Fatalf("width %d produced %d cells: %q", width, got, line)
		}
	}
}

func TestResizeRerendersDocument(t *testing.T) {
	t.Parallel()

	var gotWidth, gotHeight int
	m := newModel(Options{
		Text: "old",
		Render: func(width, height int) (string, error) {
			gotWidth, gotHeight = width, height
			return fmt.Sprintf("new %dx%d", width, height), nil
		},
		Terminal: style.Terminal{Width: 40, Height: 10},
		Theme:    style.New(false),
	})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	if gotWidth != 80 || gotHeight != 20 {
		t.Fatalf("renderer got %dx%d, want 80x20", gotWidth, gotHeight)
	}
	if content := ansi.Strip(m.viewport.GetContent()); content != "new 80x20" {
		t.Fatalf("content = %q, want new 80x20", content)
	}
}

func TestResizePreservesScroll(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("line\n", 100)
	m := newModel(Options{
		Text: long,
		Render: func(int, int) (string, error) {
			return long, nil
		},
		Terminal: style.Terminal{Width: 40, Height: 10},
		Theme:    style.New(false),
	})
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	m.viewport.GotoBottom()
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
	if !m.viewport.AtBottom() {
		t.Fatalf("resize lost bottom position at offset %d", m.viewport.YOffset())
	}

	short := newModel(Options{
		Text: "short",
		Render: func(int, int) (string, error) {
			return long, nil
		},
		Terminal: style.Terminal{Width: 40, Height: 10},
		Theme:    style.New(false),
	})
	short.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
	if short.viewport.YOffset() != 0 {
		t.Fatalf("a document that fit before resizing scrolled to %d", short.viewport.YOffset())
	}
}

func TestResizeRecomputesSearch(t *testing.T) {
	t.Parallel()

	m := newModel(Options{
		Text: "needle one\nneedle two",
		Render: func(int, int) (string, error) {
			return "prefix needle one\nneedle two", nil
		},
		Terminal: style.Terminal{Width: 40, Height: 10},
		Theme:    style.New(false),
	})
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	m.search.begin()
	for _, letter := range "needle" {
		m.handleSearchKey(tea.KeyPressMsg{Text: string(letter), Code: letter})
	}
	m.handleSearchKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.handleKey(tea.KeyPressMsg{Text: "n", Code: 'n'})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
	if len(m.search.matches) != 2 || m.search.index != 1 {
		t.Fatalf("matches = %d index = %d, want 2 and 1", len(m.search.matches), m.search.index)
	}
	want := "prefix needle one\nneedle two"
	if content := ansi.Strip(m.viewport.GetContent()); content != want {
		t.Fatalf("content = %q, want %q", content, want)
	}
	if !strings.Contains(m.viewport.GetContent(), "\x1b[") {
		t.Fatal("resize dropped search highlights")
	}
}

func TestResizeKeepsContentOnRenderError(t *testing.T) {
	t.Parallel()

	m := newModel(Options{
		Text: "stable",
		Render: func(int, int) (string, error) {
			return "", errors.New("render failed")
		},
		Terminal: style.Terminal{Width: 40, Height: 10},
		Theme:    style.New(false),
	})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	if content := ansi.Strip(m.viewport.GetContent()); content != "stable" {
		t.Fatalf("content = %q, want stable", content)
	}
}

func lineVisible(target *viewport.Model, line int) bool {
	return line >= target.YOffset() && line < target.YOffset()+target.Height()
}

func TestResizeKeepsScrollWhenMatchOffscreen(t *testing.T) {
	t.Parallel()

	text := "needle\n" + strings.Repeat("line\n", 100)
	m := newModel(Options{
		Text: text,
		Render: func(int, int) (string, error) {
			return text, nil
		},
		Terminal: style.Terminal{Width: 40, Height: 10},
		Theme:    style.New(false),
	})
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	m.search.begin()
	for _, letter := range "needle" {
		m.handleSearchKey(tea.KeyPressMsg{Text: string(letter), Code: letter})
	}
	m.handleSearchKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.viewport.GotoBottom()
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
	if !m.viewport.AtBottom() {
		t.Fatalf("resize jumped away from the bottom to offset %d", m.viewport.YOffset())
	}
}
