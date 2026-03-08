package progress

import (
	"charm.land/bubbles/v2/progress"
	"charm.land/lipgloss/v2"
	"github.com/mclucy/lucy/tools"
)

var (
	defaultOptions []progress.Option
)

func init() {
	defaultOptions = []progress.Option{
		progress.WithFillCharacters('█', '░'),
		progress.WithWidth(defaultBarWidth(0)),
	}
}

// resolveColorOptions returns color options lazily, ensuring OSC4 probing
// has been completed first. This is called at first use, not at init time.
func resolveColorOptions() []progress.Option {
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

// resolveCompleteColorOptions returns color options for completion state,
// lazily ensuring OSC4 probing has been completed first.
func resolveCompleteColorOptions() []progress.Option {
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
