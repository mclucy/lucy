// Package progress provides a terminal progress bar backed by the charm stack
// (bubbletea + bubbles/progress + lipgloss).
//
// Unlike the parent tui package which is a one-shot static renderer, this
// package uses bubbletea for live, interactive progress display.
//
// Usage:
//
//	t := progress.NewTracker("Downloading")
//	go func() {
//	    defer t.Close()
//	    resp, _ := http.Get(url)
//	    reader := t.ProxyReader(resp.Body, resp.ContentLength)
//	    io.Copy(dst, reader)
//	}()
//	if err := t.Run(); err != nil { ... }
package progress

import (
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mclucy/lucy/tools"
)

// --- Options ----------------------------------------------------------------

// Option configures a [Tracker].
type Option func(*Tracker)

// WithWidth overrides the bar width (default: 40, or auto-sized to terminal).
func WithWidth(w int) Option {
	return func(t *Tracker) { t.barWidth = w }
}

// WithGradient enables a gradient fill between two colors.
func WithGradient(colorA, colorB string) Option {
	return func(t *Tracker) { t.gradientA = colorA; t.gradientB = colorB }
}

// WithSolidFill sets a solid fill color (overrides gradient).
func WithSolidFill(color string) Option {
	return func(t *Tracker) { t.solidFill = color }
}

// WithoutPercentage hides the numeric percentage readout.
func WithoutPercentage() Option {
	return func(t *Tracker) { t.hidePercent = true }
}

// --- Tracker ----------------------------------------------------------------

// Tracker is a thread-safe progress bar controller.
//
// A Tracker is created with [NewTracker], configured with [Option] functions,
// and started with [Tracker.Run]. External goroutines update progress via
// [Tracker.SetPercent], [Tracker.IncrPercent], and [Tracker.SetMessage].
// Call [Tracker.Close] to finish and exit the progress bar.
type Tracker struct {
	title   string
	program *tea.Program

	// configuration (set before Run)
	barWidth    int
	gradientA   string
	gradientB   string
	solidFill   string
	hidePercent bool
}

// NewTracker creates a [Tracker] with the given title and options.
// Call [Tracker.Run] to display it.
func NewTracker(title string, opts ...Option) *Tracker {
	t := &Tracker{title: title}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// Run starts the progress bar and blocks until [Tracker.Close] is called
// or the user presses Ctrl+C.
func (t *Tracker) Run() error {
	// Build bubbles/progress options.
	var barOpts []progress.Option
	switch {
	case t.gradientA != "" && t.gradientB != "":
		barOpts = append(
			barOpts,
			progress.WithGradient(t.gradientA, t.gradientB),
		)
	case t.solidFill != "":
		barOpts = append(barOpts, progress.WithSolidFill(t.solidFill))
	default:
		barOpts = append(barOpts, progress.WithDefaultGradient())
	}
	if t.hidePercent {
		barOpts = append(barOpts, progress.WithoutPercentage())
	}
	if t.barWidth > 0 {
		barOpts = append(barOpts, progress.WithWidth(t.barWidth))
	}

	m := model{
		bar:   progress.New(barOpts...),
		title: t.title,
	}

	t.program = tea.NewProgram(m)
	_, err := t.program.Run()
	return err
}

// SetPercent sets the current progress to p (clamped to [0, 1]).
func (t *Tracker) SetPercent(p float64) {
	if t.program != nil {
		t.program.Send(setPercentMsg(clamp01(p)))
	}
}

// IncrPercent adds delta to the current progress.
func (t *Tracker) IncrPercent(delta float64) {
	if t.program != nil {
		t.program.Send(incrPercentMsg(delta))
	}
}

// SetMessage updates the status text shown alongside the bar.
func (t *Tracker) SetMessage(msg string) {
	if t.program != nil {
		t.program.Send(setMessageMsg(msg))
	}
}

// Close completes the progress bar (jumps to 100 %) and exits the program.
func (t *Tracker) Close() {
	if t.program != nil {
		t.program.Send(closeMsg{})
	}
}

// ProxyReader wraps r so that every Read call updates this Tracker.
// total is the expected total byte count (e.g. from Content-Length).
// If total <= 0 the bar will not be updated (indeterminate).
func (t *Tracker) ProxyReader(r io.Reader, total int64) io.Reader {
	return &proxyReader{Reader: r, tracker: t, total: total}
}

// --- bubbletea messages -----------------------------------------------------

type (
	setPercentMsg  float64
	incrPercentMsg float64
	setMessageMsg  string
	closeMsg       struct{}
)

// --- bubbletea model --------------------------------------------------------

// keyColumnWidth mirrors the constant in tui/tui_elem.go for visual alignment.
const keyColumnWidth = 16

type model struct {
	bar     progress.Model
	title   string
	message string
	percent float64
	width   int // terminal width, updated via WindowSizeMsg
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		// Auto-size bar: terminal width minus key column, message, and padding.
		barW := msg.Width - keyColumnWidth - 4
		if barW < 10 {
			barW = 10
		}
		m.bar.Width = barW
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
		m.percent = 1.0
		return m, tea.Quit

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m model) View() string {
	var sb strings.Builder

	// Title styled like tui.renderKey: bold magenta with fixed-width padding.
	title := tools.Bold(tools.Magenta(m.title))
	visualWidth := lipgloss.Width(title)
	padding := keyColumnWidth - visualWidth
	if padding < 2 {
		padding = 2
	}
	sb.WriteString(title)
	sb.WriteString(strings.Repeat(" ", padding))

	// Progress bar rendered at the current percentage.
	sb.WriteString(m.bar.ViewAs(m.percent))

	// Optional status message (dimmed).
	if m.message != "" {
		sb.WriteString("  ")
		sb.WriteString(tools.Dim(m.message))
	}

	return sb.String()
}

// --- proxyReader ------------------------------------------------------------

type proxyReader struct {
	io.Reader
	tracker *Tracker
	total   int64
	read    int64
}

func (r *proxyReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.read += int64(n)
	if r.total > 0 {
		r.tracker.SetPercent(float64(r.read) / float64(r.total))
		r.tracker.SetMessage(
			fmt.Sprintf(
				"%s / %s",
				humanBytes(r.read), humanBytes(r.total),
			),
		)
	} else {
		r.tracker.SetMessage(humanBytes(r.read))
	}
	return n, err
}

// --- helpers ----------------------------------------------------------------

func clamp01(v float64) float64 {
	return math.Max(0, math.Min(1, v))
}

func humanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}
