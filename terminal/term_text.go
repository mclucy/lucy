package terminal

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/mclucy/lucy/terminal/style"
)

func FoldHint(hidden int) string {
	return style.Muted(fmt.Sprintf("… %d more, use --long to expand", hidden))
}

func FoldLinesHint(hidden int) string {
	return style.Muted(fmt.Sprintf("… %d more lines, use --long to expand", hidden))
}

// CenterElide keeps both ends of prose and prefers complete paragraphs.
// The budget includes the omission marker; long output bypasses this function.
func CenterElide(text string, budget int) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if budget < 3 || len(lines) <= budget {
		return text
	}
	head := (budget - 1 + 1) / 2
	tail := len(lines) - (budget - 1 - head)
	// Move cuts outward from the omitted range to the nearest paragraph edge.
	// This never grows the visible output beyond its budget.
	for head > 1 && strings.TrimSpace(ansi.Strip(lines[head])) != "" {
		head--
	}
	for tail < len(lines)-1 && strings.TrimSpace(ansi.Strip(lines[tail-1])) != "" {
		tail++
	}
	if head == 1 && tail == len(lines)-1 {
		// A single paragraph has no internal boundary; retain readable ends.
		head = budget / 2
		tail = len(lines) - (budget - 1 - head)
	}
	for head > 0 && strings.TrimSpace(ansi.Strip(lines[head-1])) == "" {
		head--
	}
	for tail < len(lines) && strings.TrimSpace(ansi.Strip(lines[tail])) == "" {
		tail++
	}
	visible := append([]string{}, lines[:head]...)
	visible = append(visible, FoldLinesHint(tail-head))
	visible = append(visible, lines[tail:]...)
	return strings.Join(visible, "\n")
}

// TruncateMiddle preserves a path or reference's identifying suffix.
func TruncateMiddle(text string, width int) string {
	length := ansi.StringWidth(text)
	if width <= 0 {
		return ""
	}
	if length <= width {
		return text
	}
	if width == 1 {
		return "…"
	}
	left := (width - 1) / 2
	right := width - 1 - left
	prefix := ansi.Cut(text, 0, left)
	suffix := ansi.Cut(text, length-right, length)
	prefixWidth, suffixWidth := ansi.StringWidth(prefix), ansi.StringWidth(suffix)
	for prefixWidth+1+suffixWidth > width {
		if left > 0 {
			left--
			prefix = ansi.Cut(text, 0, left)
			prefixWidth = ansi.StringWidth(prefix)
		} else {
			right--
			suffix = ansi.Cut(text, length-right, length)
			suffixWidth = ansi.StringWidth(suffix)
		}
	}
	return prefix + "…" + suffix
}

// DisplayPath uses workspace-relative paths, then the user's home shorthand.
func DisplayPath(root, path string) string {
	if path == "" {
		return ""
	}
	if !filepath.IsAbs(path) {
		return filepath.ToSlash(filepath.Clean(path))
	}
	if relative, err := filepath.Rel(root, path); err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(relative)
	}
	if home, err := os.UserHomeDir(); err == nil {
		if relative, err := filepath.Rel(home, path); err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			if relative == "." {
				return "~"
			}
			return "~/" + filepath.ToSlash(relative)
		}
	}
	return filepath.ToSlash(path)
}
