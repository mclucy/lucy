// Package pager displays long human documents in a scrollable terminal view.
package pager

import (
	"fmt"
	"io"
	"os"
	"strings"

	"charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
	"github.com/mclucy/lucy/terminal/style"
)

// Mode selects when a document goes through the pager.
type Mode int

const (
	// Auto pages documents that overflow the terminal.
	Auto Mode = iota
	// Always pages every non-empty document.
	Always
	// Never prints documents directly.
	Never
)

// ParseMode reads the mode named by a flag value.
func ParseMode(raw string) (Mode, error) {
	switch raw {
	case "auto":
		return Auto, nil
	case "always":
		return Always, nil
	case "never":
		return Never, nil
	default:
		return Auto, fmt.Errorf("want auto, always, or never, got %q", raw)
	}
}

func (m Mode) String() string {
	switch m {
	case Always:
		return "always"
	case Never:
		return "never"
	default:
		return "auto"
	}
}

// Options describes one document and the streams that present it.
type Options struct {
	Title  string
	Text   string
	Input  io.Reader
	Output io.Writer
	// Render re-renders the document for a new terminal geometry. It must be
	// side-effect-free and cheap enough to run during a resize. Text is the
	// initial render; a nil Render only reflows the existing text.
	Render   func(width, height int) (string, error)
	Terminal style.Terminal
	Theme    style.Theme
	Mode     Mode
}

// Show presents text in a pager when options allow it, and reports whether
// the pager handled the document. A false report leaves printing to the
// caller, which keeps pipes, redirected output, and tests byte-for-byte.
func Show(options Options) (bool, error) {
	if !shouldPage(options) {
		return false, nil
	}
	program := tea.NewProgram(
		newModel(options),
		tea.WithInput(options.Input),
		tea.WithOutput(options.Output),
	)
	if _, err := program.Run(); err != nil {
		return true, fmt.Errorf("run pager: %w", err)
	}
	return true, nil
}

func shouldPage(options Options) bool {
	if !options.Terminal.IsTTY {
		return false
	}
	if !isTerminal(options.Input) || !isTerminal(options.Output) {
		return false
	}
	return needsPager(
		options.Mode,
		options.Text,
		options.Terminal.Width,
		options.Terminal.Height,
	)
}

func needsPager(mode Mode, text string, width, terminalHeight int) bool {
	if mode == Never || text == "" {
		return false
	}
	if mode == Always {
		return true
	}
	return textRows(text, width) > contentHeight(terminalHeight)
}

func isTerminal(stream any) bool {
	file, ok := stream.(*os.File)
	return ok && term.IsTerminal(file.Fd())
}

func textRows(text string, width int) int {
	width = max(1, width)
	rows := 0
	for line := range strings.SplitSeq(text, "\n") {
		rows += max(1, (ansi.StringWidth(line)+width-1)/width)
	}
	return rows
}

func contentHeight(terminalHeight int) int {
	return max(1, terminalHeight-1)
}
