package pager

import (
	"strings"
	"testing"

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
	wantOffset := m.search.matches[0].row
	if wantOffset != 60 {
		t.Fatalf("match row = %d, want 60", wantOffset)
	}
	if m.viewport.YOffset() != wantOffset {
		t.Fatalf("search scrolled to row %d, want %d", m.viewport.YOffset(), wantOffset)
	}
}

func TestSearchScrollsToMatchInsideWrappedLine(t *testing.T) {
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
	wantOffset := m.search.matches[0].row
	if wantOffset != 20 {
		t.Fatalf("match row = %d, want 20", wantOffset)
	}
	if m.viewport.YOffset() != wantOffset {
		t.Fatalf("search scrolled to row %d, want %d", m.viewport.YOffset(), wantOffset)
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

func TestResizeClampsScrollOffset(t *testing.T) {
	t.Parallel()

	lines := make([]string, 100)
	for index := range lines {
		lines[index] = strings.Repeat("x", 100)
	}
	m := newModel(Options{
		Text:     strings.Join(lines, "\n"),
		Terminal: style.Terminal{Width: 40, Height: 10},
		Theme:    style.New(false),
	})
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	m.viewport.GotoBottom()
	m.resize(120, 10)
	if m.viewport.PastBottom() {
		t.Fatalf("resize left offset %d past the document", m.viewport.YOffset())
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

func TestModelQuits(t *testing.T) {
	t.Parallel()

	m := newModel(Options{
		Text:     "content",
		Terminal: style.Terminal{Width: 20, Height: 5},
	})
	command := m.handleKey(tea.KeyPressMsg{Text: "q", Code: 'q'})
	if command == nil {
		t.Fatal("q returned no command")
	}
	if _, ok := command().(tea.QuitMsg); !ok {
		t.Fatalf("q produced %T, want tea.QuitMsg", command())
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

func lineVisible(target *viewport.Model, line int) bool {
	return line >= target.YOffset() && line < target.YOffset()+target.Height()
}
