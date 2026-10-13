package progress

import (
	"charm.land/bubbles/v2/progress"
	"charm.land/lipgloss/v2"
	"github.com/mclucy/lucy/terminal/style"
)

var globalOptions []progress.Option

func init() {
	if !style.IsTerminal {
		return
	}
	globalOptions = append(globalOptions, progress.WithFillCharacters('█', '░'))
}

// colorlessBar clears the colored defaults bubbles initializes for the filled
// and empty segments, so the bar renders without color.
func colorlessBar(m *progress.Model) {
	m.FullColor = nil
	m.EmptyColor = nil
}

// colorOptions loads terminal appearance for a new progress bar. Options are
// chosen at creation time because flags are parsed after package init.
func colorOptions() []progress.Option {
	if !style.ColorsEnabled() {
		return []progress.Option{colorlessBar}
	}
	style.EnsureTermColors()
	if style.ValidUserColors {
		return []progress.Option{
			progress.WithColors(
				style.UserColors[lipgloss.Magenta],
				style.UserColors[lipgloss.BrightMagenta],
			),
		}
	}
	return []progress.Option{progress.WithColors(lipgloss.Magenta)}
}

// successColorOptions loads terminal colors for completed entries.
func successColorOptions() []progress.Option {
	if !style.ColorsEnabled() {
		return []progress.Option{colorlessBar}
	}
	style.EnsureTermColors()
	if style.ValidUserColors {
		return []progress.Option{
			progress.WithColors(
				style.UserColors[lipgloss.Magenta],
				style.UserColors[lipgloss.Blue],
				style.UserColors[lipgloss.BrightBlue],
			),
		}
	}
	return []progress.Option{progress.WithColors(lipgloss.Blue)}
}
