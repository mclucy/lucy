package progress

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"charm.land/bubbles/v2/progress"
	tea "charm.land/bubbletea/v2"
	"github.com/mclucy/lucy/tools"
	"golang.org/x/term"
)

type entryID int

type entryState struct {
	title      string
	bar        progress.Model
	message    string
	percent    float64
	readBytes  int64
	totalBytes int64
}

type entryMsg struct {
	id      entryID
	payload tea.Msg
}

type runtimeModel struct {
	runtime *progressRuntime
}

func (m runtimeModel) Init() tea.Cmd { return nil }

func (m runtimeModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Interrupt
		}

	case tea.WindowSizeMsg:
		m.runtime.mu.Lock()
		width := defaultBarWidth(msg.Width)
		for _, entry := range m.runtime.entries {
			entry.bar.SetWidth(width)
		}
		m.runtime.mu.Unlock()

	case entryMsg:
		m.runtime.mu.Lock()
		entry, ok := m.runtime.entries[msg.id]
		if !ok {
			m.runtime.mu.Unlock()
			return m, nil
		}

		var cmd tea.Cmd
		switch payload := msg.payload.(type) {
		case setPercentMsg:
			entry.percent = float64(payload)
		case incrPercentMsg:
			entry.percent = clamp01(entry.percent + float64(payload))
		case setMessageMsg:
			entry.message = string(payload)
		case setTitleMsg:
			entry.title = string(payload)
		case bytesProgressMsg:
			if payload.total > 0 {
				entry.percent = float64(payload.read) / float64(payload.total)
			}
			entry.readBytes = payload.read
			entry.totalBytes = payload.total
		case completeMsg:
			entry.percent = 1.0
			entry.message = string(payload)
			options := append(defaultOptions, resolveCompleteColorOptions()...)
			entry.bar = progress.New(options...)
			cmd = tea.Tick(
				500*time.Millisecond, func(time.Time) tea.Msg {
					return entryMsg{id: msg.id, payload: closeMsg{}}
				},
			)
		case closeMsg:
			delete(m.runtime.entries, msg.id)
			for i, id := range m.runtime.entryOrder {
				if id == msg.id {
					m.runtime.entryOrder = append(
						m.runtime.entryOrder[:i],
						m.runtime.entryOrder[i+1:]...,
					)
					break
				}
			}
			if len(m.runtime.entries) == 0 {
				m.runtime.mu.Unlock()
				return m, tea.Quit
			}
		}
		m.runtime.mu.Unlock()
		return m, cmd
	}
	return m, nil
}

func (m runtimeModel) View() tea.View {
	m.runtime.mu.Lock()
	defer m.runtime.mu.Unlock()

	var lines []string
	for i := len(m.runtime.entryOrder) - 1; i >= 0; i-- {
		id := m.runtime.entryOrder[i]
		entry, ok := m.runtime.entries[id]
		if !ok {
			continue
		}

		var sb strings.Builder
		sb.WriteString(tools.Bold(tools.Magenta(entry.title)))
		sb.WriteString(strings.Repeat(" ", 2))
		sb.WriteString(entry.bar.ViewAs(entry.percent))

		if entry.totalBytes > 0 {
			sb.WriteString("  ")
			sb.WriteString(
				tools.Dim(
					fmt.Sprintf(
						"%s / %s",
						tools.FormatBytesBinary(entry.readBytes),
						tools.FormatBytesBinary(entry.totalBytes),
					),
				),
			)
		} else if entry.message != "" {
			sb.WriteString("  ")
			sb.WriteString(tools.Dim(entry.message))
		}

		lines = append(lines, sb.String())
	}
	return tea.NewView(strings.Join(lines, "\n"))
}

type progressRuntime struct {
	program    *tea.Program
	entries    map[entryID]*entryState
	entryOrder []entryID
	mu         sync.Mutex
	running    bool
	nextID     atomic.Int32
}

var globalRuntime = &progressRuntime{
	entries: make(map[entryID]*entryState),
}

var isTerminal = term.IsTerminal(int(os.Stdout.Fd()))

func (r *progressRuntime) registerEntry(title string) entryID {
	if !isTerminal {
		return 0
	}

	id := entryID(r.nextID.Add(1))

	r.mu.Lock()
	options := append(defaultOptions, resolveColorOptions()...)
	r.entries[id] = &entryState{
		title: title,
		bar:   progress.New(options...),
	}
	r.entryOrder = append(r.entryOrder, id)
	needStart := !r.running
	r.mu.Unlock()

	if needStart {
		r.start()
	}

	return id
}

func (r *progressRuntime) start() {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return
	}
	r.running = true
	model := runtimeModel{runtime: r}
	r.program = tea.NewProgram(model)
	r.mu.Unlock()

	go func() {
		_, err := r.program.Run()
		if errors.Is(err, tea.ErrInterrupted) {
			os.Exit(130)
		}
		r.mu.Lock()
		r.running = false
		r.program = nil
		r.mu.Unlock()
	}()
}

func (r *progressRuntime) send(id entryID, msg tea.Msg) {
	r.mu.Lock()
	running := r.running
	program := r.program
	r.mu.Unlock()

	if running && program != nil {
		program.Send(entryMsg{id: id, payload: msg})
	}
}
