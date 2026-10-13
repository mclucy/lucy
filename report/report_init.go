package report

import (
	"image/color"
	"io"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/log/v2"
)

var console = newConsole()

func newConsole() *log.Logger {
	logger := log.NewWithOptions(os.Stderr, log.Options{Level: log.InfoLevel})
	logger.SetStyles(themedStyles())
	return logger
}

func SetOutput(writer io.Writer) {
	console.SetOutput(writer)
}

func TurnOffColors() {
	// Drop only foreground colors and keep the active color profile: a NoTTY
	// profile would also strip bold and other text attributes.
	console.SetStyles(levelStyles(func(color.Color) color.Color { return lipgloss.NoColor{} }))
}

func themedStyles() *log.Styles {
	return levelStyles(nil)
}

func levelStyles(rewrite func(color.Color) color.Color) *log.Styles {
	levelStyle := func(name string, foreground color.Color) lipgloss.Style {
		if rewrite != nil {
			foreground = rewrite(foreground)
		}
		return lipgloss.NewStyle().
			SetString(strings.ToUpper(name)).
			Bold(true).
			MaxWidth(5).
			Foreground(foreground)
	}
	styles := log.DefaultStyles()
	styles.Levels[log.InfoLevel] = levelStyle(log.InfoLevel.String(), lipgloss.Green)
	styles.Levels[log.WarnLevel] = levelStyle(log.WarnLevel.String(), lipgloss.Yellow)
	styles.Levels[log.ErrorLevel] = levelStyle(log.ErrorLevel.String(), lipgloss.Red)
	styles.Levels[log.FatalLevel] = levelStyle(log.FatalLevel.String(), lipgloss.Red)
	return styles
}
