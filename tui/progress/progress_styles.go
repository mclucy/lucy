package progress

import (
	"charm.land/bubbles/v2/progress"
	"charm.land/lipgloss/v2"
	"github.com/mclucy/lucy/tools"
)

var (
	defaultOptions       []progress.Option
	defaultColor         []progress.Option
	defaultColorComplete []progress.Option
)

func init() {
	defaultOptions = []progress.Option{
		progress.WithFillCharacters('█', '░'),
		progress.WithWidth(defaultBarWidth(0)),
	}
	if tools.ValidUserColors {
		defaultColor = []progress.Option{
			progress.WithColors(
				tools.UserColors[lipgloss.Magenta],
				tools.UserColors[lipgloss.BrightMagenta],
			),
		}
		defaultColorComplete = []progress.Option{
			progress.WithColors(
				tools.UserColors[lipgloss.Magenta],
				tools.UserColors[lipgloss.Blue],
				tools.UserColors[lipgloss.BrightBlue],
			),
		}

	} else {
		defaultColor = []progress.Option{progress.WithColors(lipgloss.Magenta)}
		defaultColorComplete = []progress.Option{progress.WithColors(lipgloss.Blue)}
	}
}
