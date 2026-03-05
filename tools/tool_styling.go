package tools

import (
	"fmt"
	"image/color"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"
)

var (
	Bold      func(any) string
	Dim       func(any) string
	Italic    func(any) string
	Underline func(any) string
	Red       func(any) string
	Green     func(any) string
	Yellow    func(any) string
	Blue      func(any) string
	Magenta   func(any) string
	Cyan      func(any) string
)

var ValidUserColors bool
var UserColors = make(map[ansi.BasicColor]color.Color)

var stylesEnabled = true

func init() {
	updateStyles()
	getTermProfileColors()
}

func updateStyles() {
	if !stylesEnabled {
		noStyle := func(v any) string {
			switch v := v.(type) {
			case rune:
				return string(v)
			default:
				return fmt.Sprintf("%v", v)
			}
		}
		Bold, Dim, Italic, Underline, Red, Green, Yellow, Blue, Magenta, Cyan =
			noStyle, noStyle, noStyle, noStyle, noStyle, noStyle, noStyle, noStyle, noStyle, noStyle
		return
	}

	Bold = lsStyle(lipgloss.NewStyle().Bold(true))
	Dim = lsStyle(lipgloss.NewStyle().Faint(true))
	Italic = lsStyle(lipgloss.NewStyle().Italic(true))
	Underline = lsStyle(lipgloss.NewStyle().Underline(true))
	Red = lsStyle(lipgloss.NewStyle().Foreground(lipgloss.Red))
	Green = lsStyle(lipgloss.NewStyle().Foreground(lipgloss.Green))
	Yellow = lsStyle(lipgloss.NewStyle().Foreground(lipgloss.Yellow))
	Blue = lsStyle(lipgloss.NewStyle().Foreground(lipgloss.Blue))
	Magenta = lsStyle(lipgloss.NewStyle().Foreground(lipgloss.Magenta))
	Cyan = lsStyle(lipgloss.NewStyle().Foreground(lipgloss.Cyan))
}

// lsStyle wraps a lipgloss.Style into a func(any) string, matching the
// existing tools.Bold / tools.Dim / ... signature.
func lsStyle(s lipgloss.Style) func(any) string {
	return func(v any) string {
		switch v := v.(type) {
		case rune:
			return s.Render(string(v))
		default:
			return s.Render(fmt.Sprintf("%v", v))
		}
	}
}

func TurnOffStyles() {
	stylesEnabled = false
	updateStyles()
}

func StylesEnabled() bool {
	return stylesEnabled
}

func TermWidth() int {
	width, _, _ := term.GetSize(0)
	if width <= 0 {
		return 80
	}
	return width
}

func TermHeight() int {
	_, height, _ := term.GetSize(0)
	return height
}

func getTermProfileColors() {
	for i := ansi.BasicColor(0); i < 16; i++ {
		c := osc4Query(uint8(i))
		if c == nil {
			return
		}
		UserColors[i] = c
	}
	ValidUserColors = true
}
