// Package report presents warnings and errors on a caller-owned stream,
// independently of diagnostic logging.
package report

import (
	"image/color"
	"io"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/log/v2"
	"github.com/mclucy/lucy/terminal/style"
)

// Reporter presents one invocation's notices on the stream it owns.
type Reporter struct {
	logger *log.Logger
}

// New builds a reporter that writes level-prefixed notices to w. Machine
// modes pass io.Discard so no human notice joins their documents.
func New(w io.Writer, theme style.Theme) *Reporter {
	logger := log.NewWithOptions(w, log.Options{Level: log.InfoLevel})
	logger.SetStyles(levelStyles(theme.Colors()))
	return &Reporter{logger: logger}
}

// Info presents a non-fatal observation.
func (r *Reporter) Info(message string) {
	r.logger.Info(message)
}

// Warn presents a recoverable failure.
func (r *Reporter) Warn(err error) {
	if err != nil {
		r.logger.Warn(err.Error())
	}
}

// Error presents a failure.
func (r *Reporter) Error(err error) {
	if err != nil {
		r.logger.Error(err.Error())
	}
}

func levelStyles(colors bool) *log.Styles {
	levelStyle := func(name string, foreground color.Color) lipgloss.Style {
		level := lipgloss.NewStyle().
			SetString(strings.ToUpper(name)).
			Bold(true).
			MaxWidth(5)
		if colors {
			level = level.Foreground(foreground)
		}
		return level
	}
	styles := log.DefaultStyles()
	styles.Levels[log.InfoLevel] = levelStyle(log.InfoLevel.String(), lipgloss.Green)
	styles.Levels[log.WarnLevel] = levelStyle(log.WarnLevel.String(), lipgloss.Yellow)
	styles.Levels[log.ErrorLevel] = levelStyle(log.ErrorLevel.String(), lipgloss.Red)
	styles.Levels[log.FatalLevel] = levelStyle(log.FatalLevel.String(), lipgloss.Red)
	return styles
}
