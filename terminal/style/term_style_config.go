package style

import (
	"fmt"
	"image/color"
	"os"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
)

func init() {
	updateStyles()
}

var (
	colorsEnabled      = true
	interactiveEnabled = true
)

var (
	ValidUserColors bool
	UserColors      = make(map[ansi.BasicColor]color.Color)
)

// TurnOffColors drops ANSI colors from every style. Text attributes such as
// bold, faint, or underline survive, so output stays readable without color.
func TurnOffColors() {
	colorsEnabled = false
	updateStyles()
}

// TurnOffInteractivity disables interactive effects such as prompts and
// progress bars. Callers gate on InteractiveEnabled and fail with flag
// guidance rather than waiting on input.
func TurnOffInteractivity() {
	interactiveEnabled = false
}

// TurnOffStyles applies both halves of --no-style: --no-color and
// --no-interactive.
func TurnOffStyles() {
	TurnOffColors()
	TurnOffInteractivity()
}

// ColorsEnabled reports whether styles may carry ANSI colors.
func ColorsEnabled() bool {
	return colorsEnabled
}

func InteractiveEnabled() bool {
	return interactiveEnabled
}

// RequireInteractive fails an interactive action when stdin cannot carry a
// prompt or --no-interactive blocked it. flag names the argument carrying the
// same choice, so the error points at the way forward.
func RequireInteractive(action, flag string) error {
	if !interactiveEnabled {
		return fmt.Errorf("%s is unavailable with --no-interactive; pass %s", action, flag)
	}
	if !term.IsTerminal(os.Stdin.Fd()) {
		return fmt.Errorf("%s requires an interactive terminal; pass %s", action, flag)
	}
	return nil
}

func updateStyles() {
	// colored keeps text attributes and adds a foreground only while colors
	// are enabled, so a color-off style degrades to its bare attributes.
	colored := func(base lipgloss.Style, fg color.Color) lipgloss.Style {
		if !colorsEnabled {
			return base
		}
		return base.Foreground(fg)
	}

	Bold = lsStyle(lipgloss.NewStyle().Bold(true))
	Dim = lsStyle(lipgloss.NewStyle().Faint(true))
	Italic = lsStyle(lipgloss.NewStyle().Italic(true))
	Underline = lsStyle(lipgloss.NewStyle().Underline(true))
	Red = lsStyle(colored(lipgloss.NewStyle(), lipgloss.Red))
	Green = lsStyle(colored(lipgloss.NewStyle(), lipgloss.Green))
	Yellow = lsStyle(colored(lipgloss.NewStyle(), lipgloss.Yellow))
	Blue = lsStyle(colored(lipgloss.NewStyle(), lipgloss.Blue))
	Magenta = lsStyle(colored(lipgloss.NewStyle(), lipgloss.Magenta))
	Cyan = lsStyle(colored(lipgloss.NewStyle(), lipgloss.Cyan))

	Key = lsStyle(colored(lipgloss.NewStyle().Bold(true), lipgloss.Magenta))
	Muted = Dim
	Accent = Bold
	Success = Green
	Failure = Red
	Warning = Yellow
	Link = Underline
	Note = Cyan
}

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
