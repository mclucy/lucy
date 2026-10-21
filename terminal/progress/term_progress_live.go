package progress

import (
	"io"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/mclucy/lucy/terminal/style"
)

const (
	liveTick      = 100 * time.Millisecond
	liveLogLines  = 4
	spinnerFrames = "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"
)

var spinnerRunes = []rune(spinnerFrames)

type liveState struct {
	phase    string
	status   string
	download string
	read     int64
	total    int64
	logs     []string
}

type liveSnapshot struct {
	state liveState
	frame int
}

// live renders one animated area on stderr. Updates are coalesced by a ticker
// so byte-level progress never floods the renderer.
type live struct {
	out     io.Writer
	program *tea.Program
	done    chan struct{}
	stop    chan struct{}

	mu      sync.Mutex
	state   liveState
	frame   int
	dirty   bool
	closed  bool
	partial strings.Builder
}

func newLive(out io.Writer) *live {
	l := &live{
		out:  out,
		done: make(chan struct{}),
		stop: make(chan struct{}),
	}
	model := &liveModel{width: style.StderrWidth()}
	l.program = tea.NewProgram(
		model,
		tea.WithOutput(out),
		tea.WithInput(nil),
		tea.WithoutSignalHandler(),
	)
	go func() {
		defer close(l.done)
		_, _ = l.program.Run()
	}()
	go l.tick()
	return l
}

func (l *live) tick() {
	ticker := time.NewTicker(liveTick)
	defer ticker.Stop()
	for {
		select {
		case <-l.stop:
			return
		case <-ticker.C:
			l.mu.Lock()
			if !l.dirty || l.closed {
				l.mu.Unlock()
				continue
			}
			l.dirty = false
			l.frame++
			snapshot := liveSnapshot{state: l.state, frame: l.frame}
			l.mu.Unlock()
			l.program.Send(snapshot)
		}
	}
}

func (l *live) mutate(change func(*liveState)) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return
	}
	change(&l.state)
	l.dirty = true
}

func (l *live) PhaseStarted(text string) {
	l.mutate(func(state *liveState) {
		state.phase = text
		state.status = ""
		state.download = ""
		state.read = 0
		state.total = 0
	})
}

func (l *live) PhaseDone(text string, elapsed time.Duration) {
	l.mutate(func(state *liveState) {
		state.phase = ""
		state.status = ""
		state.download = ""
		state.read = 0
		state.total = 0
	})
	l.Println(phaseLine(text, elapsed))
}

func (l *live) Status(text string) {
	l.mutate(func(state *liveState) { state.status = text })
}

func (l *live) Download(name string, read, total int64) {
	l.mutate(func(state *liveState) {
		state.download = name
		state.read = read
		state.total = total
	})
}

func (l *live) DownloadDone() {
	l.mutate(func(state *liveState) {
		state.download = ""
		state.read = 0
		state.total = 0
	})
}

func (l *live) Println(lines ...string) {
	if len(lines) == 0 {
		return
	}
	l.mu.Lock()
	closed := l.closed
	l.mu.Unlock()
	if closed {
		_, _ = io.WriteString(l.out, strings.Join(lines, "\n")+"\n")
		return
	}
	l.program.Println(strings.Join(lines, "\n"))
}

func (l *live) LogWriter() io.Writer { return logTail{l} }

func (l *live) appendLog(line string) {
	l.mutate(func(state *liveState) {
		state.logs = append(state.logs, line)
		if len(state.logs) > liveLogLines {
			state.logs = state.logs[len(state.logs)-liveLogLines:]
		}
	})
}

func (l *live) Close() {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return
	}
	l.closed = true
	if l.partial.Len() > 0 {
		line := l.partial.String()
		l.partial.Reset()
		l.state.logs = append(l.state.logs, line)
	}
	l.mu.Unlock()
	close(l.stop)
	l.program.Quit()
	<-l.done
}

type liveModel struct {
	state liveState
	frame int
	width int
}

func (m *liveModel) Init() tea.Cmd { return nil }

func (m *liveModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case liveSnapshot:
		m.state = msg.state
		m.frame = msg.frame
	case tea.WindowSizeMsg:
		m.width = msg.Width
	}
	return m, nil
}

func (m *liveModel) View() tea.View {
	width := m.width
	if width <= 0 {
		width = style.StderrWidth()
	}
	var lines []string
	for _, line := range m.state.logs {
		lines = append(lines, "  "+style.Muted("│ "+line))
	}
	if m.state.phase != "" {
		frame := spinnerRunes[m.frame%len(spinnerRunes)]
		head := style.Note(string(frame)) + " " + style.Accent(m.state.phase)
		if m.state.status != "" {
			available := width - lipgloss.Width(head) - 3
			status := m.state.status
			if available >= 0 && lipgloss.Width(status) > available {
				status = ansi.Truncate(status, available, "…")
			}
			head += style.Muted(" · " + status)
		}
		lines = append(lines, clip(head, width))
	}
	if m.state.download != "" {
		lines = append(lines, clip(m.downloadLine(width), width))
	}
	return tea.NewView(strings.Join(lines, "\n"))
}

func (m *liveModel) downloadLine(width int) string {
	read := style.FormatBytesDecimal(m.state.read)
	total := style.FormatBytesDecimal(m.state.total)
	values := read + " / " + total
	suffix := "  " + style.Muted(values)
	prefix := "  " + m.state.download + "  "
	barWidth := width - lipgloss.Width(prefix) - lipgloss.Width(values) - 2
	if barWidth < 8 {
		barWidth = 8
	}
	percent := 0.0
	if m.state.total > 0 {
		percent = float64(m.state.read) / float64(m.state.total)
		if percent > 1 {
			percent = 1
		}
	}
	filled := int(percent * float64(barWidth))
	bar := style.Note(strings.Repeat("█", filled)) +
		style.Muted(strings.Repeat("░", barWidth-filled))
	return prefix + bar + suffix
}

func clip(line string, width int) string {
	if lipgloss.Width(line) <= width {
		return line
	}
	return ansi.Truncate(line, width, "")
}

// logTail converts a subprocess's writes into whole lines for the live area.
type logTail struct{ renderer *live }

func (w logTail) Write(data []byte) (int, error) {
	w.renderer.mu.Lock()
	w.renderer.partial.Write(data)
	text := w.renderer.partial.String()
	var lines []string
	if index := strings.LastIndexByte(text, '\n'); index >= 0 {
		lines = strings.Split(text[:index], "\n")
		w.renderer.partial.Reset()
		w.renderer.partial.WriteString(text[index+1:])
	}
	if len(lines) > 0 {
		w.renderer.mu.Unlock()
		for _, line := range lines {
			if strings.TrimSpace(line) != "" {
				w.renderer.appendLog(line)
			}
		}
		return len(data), nil
	}
	w.renderer.mu.Unlock()
	return len(data), nil
}
