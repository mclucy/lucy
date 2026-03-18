// Package progress runtime manages the bubbletea program lifecycle.
//
// # Lifecycle States
//
// The runtime transitions through states: idle -> running -> stopped.
// The stopped flag is set on interrupt (Ctrl+C) or when all entries complete.
// After all-complete shutdown, new tracker registration resets stopped and restarts.
//
// # Graceful Interrupt
//
// On Ctrl+C, the runtime sets the stopped atomic flag and returns control
// to the caller. The runtime does not call os.Exit - the caller controls
// process lifecycle.
//
// # Idempotent Shutdown
//
// The stopped atomic flag ensures shutdown operations are idempotent.
// Multiple Close() calls or interrupts are safe. Defer-based cleanup in
// the runtime goroutine ensures fields reset on all exit paths.
package progress

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"charm.land/bubbles/v2/progress"
	tea "charm.land/bubbletea/v2"
	"github.com/mclucy/lucy/tools"
)

type entryID int

type entryState struct {
	title      string
	bar        progress.Model
	message    string
	percent    float64
	readBytes  int64
	totalBytes int64
	logLines   []string
	partialLog string
	logCap     int
	completed  bool
}

type entryMsg struct {
	id      entryID
	payload tea.Msg
}

type runtime struct {
	program    *tea.Program
	entries    map[entryID]*entryState
	entryOrder []entryID
	mu         sync.Mutex
	running    bool
	nextID     atomic.Int32
	done       chan struct{}
	stopped    atomic.Bool
}

func (m *runtime) Init() tea.Cmd { return nil }

func (m *runtime) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Interrupt
		}

	case tea.WindowSizeMsg:
		m.mu.Lock()
		width := getTrackerWidth(msg.Width)
		for _, entry := range m.entries {
			titleWidth := len(entry.title) + 2
			entry.bar.SetWidth(width - titleWidth)
		}
		m.mu.Unlock()

	case entryMsg:
		m.mu.Lock()
		entry, ok := m.entries[msg.id]
		if !ok {
			m.mu.Unlock()
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
			width := entry.bar.Width()
			titleWidth := len(entry.title) + 2
			entry.bar.SetWidth(width - titleWidth)
		case bytesProgressMsg:
			if payload.total > 0 {
				entry.percent = float64(payload.read) / float64(payload.total)
			}
			entry.readBytes = payload.read
			entry.totalBytes = payload.total
		case appendLogMsg:
			entry.partialLog += string(payload)
			lines := strings.Split(entry.partialLog, "\n")
			if len(lines) > 1 {
				entry.logLines = append(entry.logLines, lines[:len(lines)-1]...)
				entry.partialLog = lines[len(lines)-1]
				if entry.logCap > 0 && len(entry.logLines) > entry.logCap {
					entry.logLines = entry.logLines[len(entry.logLines)-entry.logCap:]
				}
			}
		case completeMsg:
			entry.percent = 1.0
			entry.message = string(payload)
			entry.completed = true
			// order sensitive, set success colors last so they override global options
			options := append(globalOptions, successColorOptions()...)
			entry.bar = progress.New(options...)
			if m.allCompleted() {
				m.mu.Unlock()
				return m, tea.Quit
			}
		case closeMsg:
			entry.completed = true
			if m.allCompleted() {
				m.mu.Unlock()
				return m, tea.Quit
			}
		}
		m.mu.Unlock()
		return m, cmd
	}
	return m, nil
}

func (m *runtime) View() tea.View {
	m.mu.Lock()
	defer m.mu.Unlock()

	var lines []string
	for i := len(m.entryOrder) - 1; i >= 0; i-- {
		id := m.entryOrder[i]
		entry, ok := m.entries[id]
		if !ok {
			continue
		}

		for _, logLine := range entry.logLines {
			lines = append(lines, tools.Dim(logLine))
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
		} else {
			sb.WriteString("  ")
			sb.WriteString(tools.Dim(fmt.Sprintf("%.1f%%", entry.percent*100)))
		}

		lines = append(lines, sb.String())
	}
	return tea.NewView(strings.Join(lines, "\n"))
}

func (m *runtime) allCompleted() bool {
	for _, entry := range m.entries {
		if !entry.completed {
			return false
		}
	}
	return len(m.entries) > 0
}

var globalRuntime = &runtime{
	entries: make(map[entryID]*entryState),
}

func (r *runtime) registerEntry(title string, logCapacity int) entryID {
	if !tools.IsTerminal {
		return 0
	}

	r.mu.Lock()
	canRestart := len(r.entries) == 0 || r.allCompleted()
	r.mu.Unlock()

	if r.stopped.Load() && !canRestart {
		return 0
	}

	if r.stopped.Load() && canRestart {
		r.stopped.Store(false)
	}

	id := entryID(r.nextID.Add(1))
	width := getTrackerWidth(0)
	titleWidth := len(title) + 2
	barWidth := width - titleWidth
	widthOption := progress.WithWidth(barWidth)
	options := append(globalOptions, widthOption)
	r.mu.Lock()
	r.entries[id] = &entryState{
		title:  title,
		bar:    progress.New(options...),
		logCap: logCapacity,
	}
	r.entryOrder = append(r.entryOrder, id)
	needStart := !r.running
	r.mu.Unlock()

	if needStart && !r.stopped.Load() {
		r.start()
	}

	return id
}

func (r *runtime) start() {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return
	}
	r.running = true
	r.done = make(chan struct{})
	r.program = tea.NewProgram(r)
	r.mu.Unlock()

	go func() {
		defer close(r.done)
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

func (r *runtime) send(id entryID, msg tea.Msg) {
	if r.stopped.Load() {
		return
	}

	r.mu.Lock()
	running := r.running
	program := r.program
	r.mu.Unlock()

	if running && program != nil {
		program.Send(entryMsg{id: id, payload: msg})
	}
}

func (r *runtime) waitForShutdown(ctx context.Context) error {
	r.mu.Lock()
	done := r.done
	r.mu.Unlock()

	if done == nil {
		return nil
	}

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
