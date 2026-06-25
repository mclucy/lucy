package style

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"charm.land/glamour/v2"
	"charm.land/glamour/v2/styles"
	"charm.land/lipgloss/v2"
	"github.com/mclucy/lucy/internal/fn"
)

const CRLF = "\r\n"

func MarkdownToAnsi(md string, maxWidth int) string {
	trimmed := strings.TrimSpace(md)
	if trimmed == "" {
		return ""
	}
	if maxWidth <= 0 {
		maxWidth = TermWidth()
	}

	options := []glamour.TermRendererOption{
		glamour.WithWordWrap(maxWidth),
	}
	if StylesEnabled() {
		if HasDarkBackground() {
			options = append(options, glamour.WithStandardStyle(styles.DarkStyle))
		} else {
			options = append(options, glamour.WithStandardStyle(styles.LightStyle))
		}
	} else {
		options = append(options, glamour.WithStandardStyle(styles.NoTTYStyle))
	}

	renderer, err := glamour.NewTermRenderer(options...)
	defer fn.CloseReader(renderer, func(err error) {})
	if err != nil {
		return trimmed
	}
	rendered, err := renderer.Render(trimmed)
	if err != nil {
		return trimmed
	}
	rendered = strings.TrimRight(rendered, "\n")
	lines := strings.Split(rendered, "\n")
	for i, line := range lines {
		if lipgloss.Width(line) > maxWidth {
			lines[i] = lipgloss.Wrap(line, maxWidth, "")
		}
	}
	return strings.Join(lines, "\n")
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

func PrintAsJsonCompact(v interface{}) {
	data, err := json.Marshal(v)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(string(data))
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
