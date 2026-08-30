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

// colorOptions returns color options lazily, ensuring OSC4 probing
// has been completed first. This is called at first use, not at init time.
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

// successColorOptions returns color options for success state,
// lazily ensuring OSC4 probing has been completed first.
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
