package upstream

import (
	"strings"
	"unicode"
)

// LooksLikeMarkdown reports whether text carries Markdown-only structure:
// ATX headings, fenced code, list items, blockquotes, or inline code, links,
// and emphasis. Plain prose, including bare URLs, is not Markdown.
//
// Detection is purely lexical: upstream metadata normalization must not depend
// on a presentation renderer.
func LooksLikeMarkdown(text string) bool {
	for line := range strings.Lines(text) {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "```") ||
			strings.HasPrefix(trimmed, "~~~") {
			return true
		}
		if isATXHeading(trimmed) ||
			isListItem(trimmed) ||
			strings.HasPrefix(trimmed, "> ") ||
			trimmed == ">" {
			return true
		}
		if hasInlineMarkdown(trimmed) {
			return true
		}
	}
	return false
}

func isATXHeading(line string) bool {
	level := 0
	for level < len(line) && line[level] == '#' {
		level++
	}
	if level == 0 || level > 6 {
		return false
	}
	return level == len(line) || line[level] == ' ' || line[level] == '\t'
}

func isListItem(line string) bool {
	for _, marker := range []string{"- ", "* ", "+ "} {
		if strings.HasPrefix(line, marker) {
			return true
		}
	}
	digits := 0
	for digits < len(line) && unicode.IsDigit(rune(line[digits])) {
		digits++
	}
	return digits > 0 &&
		digits+1 < len(line) &&
		(line[digits] == '.' || line[digits] == ')') &&
		line[digits+1] == ' '
}

func hasInlineMarkdown(line string) bool {
	if strings.Contains(line, "`") {
		return true
	}
	if strings.Contains(line, "](http://") || strings.Contains(line, "](https://") {
		return true
	}
	if strings.Contains(line, "**") || strings.Contains(line, "__") {
		return true
	}
	// A single asterisk pair around non-space text is emphasis; a lone
	// asterisk is ordinary punctuation.
	if first := strings.IndexByte(line, '*'); first >= 0 {
		rest := line[first+1:]
		if second := strings.IndexByte(rest, '*'); second > 0 {
			inner := rest[:second]
			if strings.TrimSpace(inner) == inner && inner != "" {
				return true
			}
		}
	}
	return false
}
