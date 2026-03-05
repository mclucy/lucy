package tools

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
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

var stylesEnabled = true

func init() {
	renewStyleFunctions()
}

func renewStyleFunctions() {
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
	Red = lsStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("1")))
	Green = lsStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("2")))
	Yellow = lsStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("3")))
	Blue = lsStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("4")))
	Magenta = lsStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("5")))
	Cyan = lsStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("6")))
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
	renewStyleFunctions()
}

func StylesEnabled() bool {
	return stylesEnabled
}

// PrintAsJson is usually used for debugging purposes
func PrintAsJson(v interface{}) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(string(data))
}

func TermWidth() int {
	width, _, _ := term.GetSize(0)
	return width
}

func TermHeight() int {
	_, height, _ := term.GetSize(0)
	return height
}

func Capitalize(v any) string {
	s, ok := v.(string)
	if !ok {
		s = fmt.Sprintf("%v", v)
	}
	if len(s) == 0 {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func FormatBytesBinary(bytes int64) string {
	const (
		kib = 1024
		mib = kib * 1024
		gib = mib * 1024
	)
	switch {
	case bytes >= gib:
		return fmt.Sprintf("%.1f GiB", float64(bytes)/float64(gib))
	case bytes >= mib:
		return fmt.Sprintf("%.1f MiB", float64(bytes)/float64(mib))
	case bytes >= kib:
		return fmt.Sprintf("%.1f KiB", float64(bytes)/float64(kib))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

func FormatBytesDecimal(bytes int64) string {
	const (
		kb = 1000
		mb = kb * 1000
		gb = mb * 1000
	)
	switch {
	case bytes >= gb:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(gb))
	case bytes >= mb:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(mb))
	case bytes >= kb:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(kb))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

func FormatDuration(t time.Time) string {
	remaining := time.Until(t)
	if remaining <= 0 {
		return "expired"
	}
	switch {
	case remaining >= 24*time.Hour:
		days := int(remaining.Hours() / 24)
		return fmt.Sprintf("expires in %dd", days)
	case remaining >= time.Hour:
		return fmt.Sprintf("expires in %dh", int(remaining.Hours()))
	default:
		return fmt.Sprintf("expires in %dm", int(remaining.Minutes()))
	}
}
