package upstream

import (
	"strings"

	"github.com/mclucy/lucy/tools"
)

// LooksLikeMarkdown returns true when rendering the text as markdown produces a
// meaningfully different result than showing the original plain text.
func LooksLikeMarkdown(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}

	rendered := strings.TrimSpace(tools.MarkdownToAnsi(trimmed, 80))
	if rendered == "" || rendered == trimmed {
		return false
	}

	plain := normalizeMarkdownDetectionText(trimmed)
	markdown := normalizeMarkdownDetectionText(rendered)
	if markdown == "" || markdown == plain {
		return false
	}

	return true
}

func normalizeMarkdownDetectionText(text string) string {
	fields := strings.Fields(text)
	return strings.Join(fields, " ")
}
