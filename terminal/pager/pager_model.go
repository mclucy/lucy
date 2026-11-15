package pager

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/viewport"
	"charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/mclucy/lucy/terminal/style"
)

// model owns one document's scroll position, search state, and footer.
type model struct {
	viewport viewport.Model
	title    string
	text     string
	theme    style.Theme
	width    int
	height   int
	search   searchState
	hint     string
}

func newModel(options Options) *model {
	width := max(1, options.Terminal.Width)
	height := max(2, options.Terminal.Height)
	m := &model{
		title:  options.Title,
		text:   options.Text,
		theme:  options.Theme,
		width:  width,
		height: height,
	}
	m.viewport = viewport.New(
		viewport.WithWidth(width),
		viewport.WithHeight(contentHeight(height)),
	)
	m.viewport.SoftWrap = true
	m.viewport.MouseWheelEnabled = false
	m.viewport.SetContent(options.Text)
	return m
}

func (m *model) Init() tea.Cmd { return nil }

func (m *model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		m.resize(message.Width, message.Height)
		return m, nil
	case tea.KeyPressMsg:
		return m, m.handleKey(message)
	case tea.PasteMsg:
		if m.search.active {
			m.search.paste(message.Content)
		}
		return m, nil
	}
	return m, nil
}

func (m *model) View() tea.View {
	view := tea.NewView(m.viewport.View() + "\n" + m.footer())
	view.AltScreen = true
	return view
}

func (m *model) handleKey(key tea.KeyPressMsg) tea.Cmd {
	if m.search.active {
		m.hint = ""
		return m.handleSearchKey(key)
	}
	if key.String() != "esc" {
		m.hint = ""
	}
	switch key.String() {
	case "q", "ctrl+c":
		return tea.Quit
	case "esc":
		m.hint = "press q to quit"
		return nil
	case "g", "home":
		m.viewport.GotoTop()
		return nil
	case "G", "end":
		m.viewport.GotoBottom()
		return nil
	case "/":
		m.search.begin()
		return nil
	case "n":
		m.search.next(1)
		m.refreshSearch()
		return nil
	case "N":
		m.search.next(-1)
		m.refreshSearch()
		return nil
	}
	var command tea.Cmd
	m.viewport, command = m.viewport.Update(key)
	return command
}

func (m *model) handleSearchKey(key tea.KeyPressMsg) tea.Cmd {
	switch key.String() {
	case "esc", "ctrl+c":
		m.search.cancel()
	case "enter":
		m.search.submit(&m.viewport, m.text, m.width)
		m.refreshSearch()
	case "backspace":
		m.search.backspace()
	case "ctrl+u":
		m.search.query = ""
	default:
		m.search.typeText(key.Text)
	}
	return nil
}

// refreshSearch applies the committed matches to the viewport and reveals the
// selected match.
func (m *model) refreshSearch() {
	m.viewport.SetContent(
		highlightMatches(m.text, m.search.matches, m.search.index, m.theme),
	)
	if len(m.search.matches) > 0 {
		showMatch(&m.viewport, m.search.matches[m.search.index])
	}
}

func (m *model) resize(width, height int) {
	m.width = max(1, width)
	m.height = max(2, height)
	m.viewport.SetWidth(m.width)
	m.viewport.SetHeight(contentHeight(m.height))
	m.search.rewrap(m.text, m.width)
	// Dimension setters keep the old offset; resetting it clamps to the new
	// wrapped height so a wider terminal cannot leave a blank view.
	m.viewport.SetYOffset(m.viewport.YOffset())
}

func (m *model) footer() string {
	if m.search.active {
		return fitStatus(
			m.theme.Accent("/"+m.search.query+"\u258f"),
			[]string{m.theme.Muted("enter search  esc cancel")},
			m.width,
		)
	}
	if m.hint != "" {
		return fitStatus(
			m.theme.Accent(m.title),
			[]string{m.theme.Warning(m.hint)},
			m.width,
		)
	}
	hints := "q quit  / search  j/k scroll"
	if m.search.term != "" {
		hints = m.search.label() + "  n/N next/prev"
	}
	return fitStatus(
		m.theme.Accent(m.title),
		[]string{m.theme.Muted(hints), fmt.Sprintf("%3d%%", int(m.viewport.ScrollPercent()*100))},
		m.width,
	)
}

// fitStatus drops the least important right-hand piece first, keeping the
// title and position visible in narrow terminals.
func fitStatus(left string, right []string, width int) string {
	for len(right) > 0 {
		candidate := strings.Join(right, "  ")
		if ansi.StringWidth(left)+ansi.StringWidth(candidate)+1 <= width {
			return fitLine(left, candidate, width)
		}
		right = right[1:]
	}
	if ansi.StringWidth(left) > width {
		left = ansi.Truncate(left, width, "\u2026")
	}
	return left + strings.Repeat(" ", max(0, width-ansi.StringWidth(left)))
}

// fitLine places left and right on one width-limited line. The right side
// carries the live state, so the left label yields first when space runs out.
func fitLine(left, right string, width int) string {
	leftWidth, rightWidth := ansi.StringWidth(left), ansi.StringWidth(right)
	if leftWidth+rightWidth+1 > width {
		left, leftWidth = "", 0
	}
	if leftWidth+rightWidth+1 > width {
		right = ansi.Truncate(right, max(0, width-leftWidth-1), "\u2026")
		rightWidth = ansi.StringWidth(right)
	}
	return left + strings.Repeat(" ", max(0, width-leftWidth-rightWidth)) + right
}
