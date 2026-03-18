package progress

import (
	"charm.land/bubbles/v2/progress"
	"charm.land/lipgloss/v2"
	"github.com/mclucy/lucy/tools"
)

var globalOptions []progress.Option

func init() {
	if !tools.IsTerminal {
		return
	}
	globalOptions = append(
		colorOptions(),
		progress.WithFillCharacters('█', '░'),
	)
}

// colorOptions returns color options lazily, ensuring OSC4 probing
// has been completed first. This is called at first use, not at init time.
func colorOptions() []progress.Option {
	tools.EnsureTermColors()
	if tools.ValidUserColors {
		return []progress.Option{
			progress.WithColors(
				tools.UserColors[lipgloss.Magenta],
				tools.UserColors[lipgloss.BrightMagenta],
			),
		}
	}
	return []progress.Option{progress.WithColors(lipgloss.Magenta)}
}

// successColorOptions returns color options for success state,
// lazily ensuring OSC4 probing has been completed first.
func successColorOptions() []progress.Option {
	tools.EnsureTermColors()
	if tools.ValidUserColors {
		return []progress.Option{
			progress.WithColors(
				tools.UserColors[lipgloss.Magenta],
				tools.UserColors[lipgloss.Blue],
				tools.UserColors[lipgloss.BrightBlue],
			),
		}
	}
	return []progress.Option{progress.WithColors(lipgloss.Blue)}
}
