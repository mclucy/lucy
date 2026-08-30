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

// colorOptions loads terminal colors on first use.
func colorOptions() []progress.Option {
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
