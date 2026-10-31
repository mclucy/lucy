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

// livePrint asks the model to emit a permanent line above the live area. The
// model closes done once the line is queued for rendering.
type livePrint struct {
	text string
	done chan struct{}
}

// live renders one animated area on stderr. Updates are coalesced by a ticker
// so byte-level progress never floods the renderer.
type live struct {
	out     io.Writer
	theme   style.Theme
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

func newLive(options Options) *live {
	width := options.Terminal.Width
	if width <= 0 {
		width = style.DefaultTerminalWidth
	}
	l := &live{
		out:   options.Writer,
		theme: options.Theme,
		done:  make(chan struct{}),
		stop:  make(chan struct{}),
	}
	model := &liveModel{width: width, theme: options.Theme}
	l.program = tea.NewProgram(
		model,
		tea.WithOutput(options.Writer),
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
	l.Println(phaseLine(text, elapsed, l.theme))
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
	text := strings.Join(lines, "\n")
	// The lock stays held until the model acknowledges the line, so Close
	// cannot quit the program while a permanent line is still in flight.
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		// The program owns the terminal until it stops. Writing past it while
		// the final frame is still being erased would interleave the two.
		<-l.done
		_, _ = io.WriteString(l.out, text+"\n")
		return
	}
	done := make(chan struct{})
	// Send, unlike Program.Println, returns when the program has stopped, so a
	// late line cannot block shutdown.
	l.program.Send(livePrint{text: text, done: done})
	select {
	case <-done:
	case <-l.done:
	}
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
		// Another caller is already stopping the program. Waiting for it keeps
		// later output from racing with its final frame.
		<-l.done
		return
	}
	l.closed = true
	partial := l.partial.String()
	l.partial.Reset()
	l.mu.Unlock()

	close(l.stop)
	l.program.Quit()
	<-l.done

	// The model learns log lines only from snapshots, so a trailing line
	// without its newline terminator has no frame left to appear in. Emit it
	// once as permanent output now that the program released the terminal.
	if strings.TrimSpace(partial) != "" {
		l.mu.Lock()
		_, _ = io.WriteString(l.out, liveLogLine(l.theme, partial)+"\n")
		l.mu.Unlock()
	}
}

// liveLogLine formats one subprocess line the way the animated area shows it,
// so a line flushed at shutdown keeps the same shape.
func liveLogLine(theme style.Theme, line string) string {
	return "  " + theme.Muted("│ "+line)
}

type liveModel struct {
	state liveState
	frame int
	width int
	theme style.Theme
}

func (m *liveModel) Init() tea.Cmd { return nil }

func (m *liveModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case liveSnapshot:
		m.state = msg.state
		m.frame = msg.frame
	case livePrint:
		return m, tea.Sequence(tea.Println(msg.text), func() tea.Msg {
			close(msg.done)
			return nil
		})
	case tea.WindowSizeMsg:
		m.width = msg.Width
	}
	return m, nil
}

func (m *liveModel) View() tea.View {
	width := m.width
	if width <= 0 {
		width = style.DefaultTerminalWidth
	}
	var lines []string
	for _, line := range m.state.logs {
		lines = append(lines, liveLogLine(m.theme, line))
	}
	if m.state.phase != "" {
		frame := spinnerRunes[m.frame%len(spinnerRunes)]
		head := m.theme.Note(string(frame)) + " " + m.theme.Accent(m.state.phase)
		if m.state.status != "" {
			available := width - lipgloss.Width(head) - 2
			status := m.state.status
			if available >= 0 && lipgloss.Width(status) > available {
				status = ansi.Truncate(status, available, "…")
			}
			head += m.theme.Muted(": " + status)
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
	suffix := "  " + m.theme.Muted(values)
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
	bar := m.theme.Note(strings.Repeat("█", filled)) +
		m.theme.Muted(strings.Repeat("░", barWidth-filled))
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
	for _, line := range w.renderer.bufferLog(data) {
		if strings.TrimSpace(line) != "" {
			w.renderer.appendLog(line)
		}
	}
	return len(data), nil
}

// bufferLog appends a subprocess chunk and returns the whole lines it
// completed. A closed renderer drops the chunk, as plain output does, so a
// late write cannot reach the terminal after the final frame.
func (l *live) bufferLog(data []byte) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.partial.Write(data)
	complete, remainder, found := strings.CutLast(l.partial.String(), "\n")
	if !found {
		return nil
	}
	lines := strings.Split(complete, "\n")
	l.partial.Reset()
	l.partial.WriteString(remainder)
	return lines
}
