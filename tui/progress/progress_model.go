package progress

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/progress"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/mclucy/lucy/tools"
)

type (
	setPercentMsg    float64
	incrPercentMsg   float64
	setMessageMsg    string
	closeMsg         struct{}
	completeMsg      string
	bytesProgressMsg struct {
		read  int64
		total int64
	}
)

type model struct {
	bar        progress.Model
	title      string
	message    string
	percent    float64
	readBytes  int64
	totalBytes int64
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.bar.SetWidth(defaultBarWidth(msg.Width))
		return m, nil

	case bytesProgressMsg:
		if msg.total > 0 {
			m.percent = float64(msg.read) / float64(msg.total)
		}
		m.readBytes = msg.read
		m.totalBytes = msg.total
		return m, nil

	case setPercentMsg:
		m.percent = float64(msg)
		return m, nil

	case incrPercentMsg:
		m.percent = clamp01(m.percent + float64(msg))
		return m, nil

	case setMessageMsg:
		m.message = string(msg)
		return m, nil

	case closeMsg:
		return m, tea.Quit

	case completeMsg:
		m.percent = 1.0
		m.message = string(msg)
		m.bar = progress.New(
			progress.WithColors(
				lipgloss.Green,
				lipgloss.BrightGreen,
			),
		)
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Interrupt
		}
	}
	return m, nil
}

func (m model) View() tea.View {
	var sb strings.Builder

	// Title styled like tui.renderKey: bold magenta with fixed-width padding.
	title := tools.Bold(tools.Magenta(m.title))
	sb.WriteString(title)
	sb.WriteString(strings.Repeat(" ", 2))

	// Progress bar rendered at the current percentage.
	sb.WriteString(m.bar.ViewAs(m.percent))

	// Byte progress text takes priority over arbitrary message.
	if m.totalBytes > 0 {
		sb.WriteString("  ")
		sb.WriteString(
			tools.Dim(
				fmt.Sprintf(
					"%s / %s",
					tools.FormatBytesBinary(m.readBytes),
					tools.FormatBytesBinary(m.totalBytes),
				),
			),
		)
	} else if m.message != "" {
		sb.WriteString("  ")
		sb.WriteString(tools.Dim(m.message))
	}

	return tea.NewView(sb.String())
}
