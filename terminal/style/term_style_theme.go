package style

import (
	"fmt"
	"image/color"

	"charm.land/lipgloss/v2"
)

// Theme is the immutable presentation style for one invocation. Construct it
// with New; every method returns rendered text and never mutates the theme.
// The zero value renders plain text without attributes.
type Theme struct {
	colors bool

	bold      lipgloss.Style
	dim       lipgloss.Style
	italic    lipgloss.Style
	underline lipgloss.Style
	red       lipgloss.Style
	green     lipgloss.Style
	yellow    lipgloss.Style
	blue      lipgloss.Style
	magenta   lipgloss.Style
	cyan      lipgloss.Style
	key       lipgloss.Style
}

// New builds a theme. When colors is false, foreground colors are dropped
// while text attributes such as bold, faint, italic, and underline survive,
// so --no-color output stays readable.
func New(colors bool) Theme {
	colored := func(fg color.Color) lipgloss.Style {
		base := lipgloss.NewStyle()
		if !colors {
			return base
		}
		return base.Foreground(fg)
	}
	return Theme{
		colors:    colors,
		bold:      lipgloss.NewStyle().Bold(true),
		dim:       lipgloss.NewStyle().Faint(true),
		italic:    lipgloss.NewStyle().Italic(true),
		underline: lipgloss.NewStyle().Underline(true),
		red:       colored(lipgloss.Red),
		green:     colored(lipgloss.Green),
		yellow:    colored(lipgloss.Yellow),
		blue:      colored(lipgloss.Blue),
		magenta:   colored(lipgloss.Magenta),
		cyan:      colored(lipgloss.Cyan),
		key:       colored(lipgloss.Magenta).Bold(true),
	}
}

// Colors reports whether the theme may carry ANSI foreground colors.
func (t Theme) Colors() bool { return t.colors }

func (t Theme) Bold(v any) string      { return renderStyle(t.bold, v) }
func (t Theme) Dim(v any) string       { return renderStyle(t.dim, v) }
func (t Theme) Italic(v any) string    { return renderStyle(t.italic, v) }
func (t Theme) Underline(v any) string { return renderStyle(t.underline, v) }
func (t Theme) Red(v any) string       { return renderStyle(t.red, v) }
func (t Theme) Green(v any) string     { return renderStyle(t.green, v) }
func (t Theme) Yellow(v any) string    { return renderStyle(t.yellow, v) }
func (t Theme) Blue(v any) string      { return renderStyle(t.blue, v) }
func (t Theme) Magenta(v any) string   { return renderStyle(t.magenta, v) }
func (t Theme) Cyan(v any) string      { return renderStyle(t.cyan, v) }

// Semantic roles — use these instead of raw colors.

// Key styles KV field labels (bold magenta).
func (t Theme) Key(v any) string { return renderStyle(t.key, v) }

// Muted styles secondary information and annotations (faint).
func (t Theme) Muted(v any) string { return renderStyle(t.dim, v) }

// Accent styles emphasized names and highlights (bold).
func (t Theme) Accent(v any) string { return renderStyle(t.bold, v) }

// Success styles checkmarks and completion (green).
func (t Theme) Success(v any) string { return renderStyle(t.green, v) }

// Failure styles error indicators (red).
func (t Theme) Failure(v any) string { return renderStyle(t.red, v) }

// Warning styles warnings (yellow).
func (t Theme) Warning(v any) string { return renderStyle(t.yellow, v) }

// Link styles URLs (underline).
func (t Theme) Link(v any) string { return renderStyle(t.underline, v) }

// Note styles informational markers (cyan).
func (t Theme) Note(v any) string { return renderStyle(t.cyan, v) }

func renderStyle(s lipgloss.Style, v any) string {
	switch v := v.(type) {
	case rune:
		return s.Render(string(v))
	default:
		return s.Render(fmt.Sprintf("%v", v))
	}
}
