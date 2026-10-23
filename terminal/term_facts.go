package terminal

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/mclucy/lucy/terminal/style"
)

// Fact is a human-facing label with a possibly multiline value.
type Fact struct {
	Key   string
	Value string
}

// RenderFacts uses one label column for the entire document.
func RenderFacts(facts []Fact, width int) string {
	keyWidth := 0
	for _, fact := range facts {
		keyWidth = max(keyWidth, ansi.StringWidth(fact.Key))
	}
	keyWidth = min(keyWidth, max(1, width/3))
	valueWidth := max(1, width-keyWidth-2)
	indent := strings.Repeat(" ", keyWidth+2)
	var output strings.Builder
	for _, fact := range facts {
		key := TruncateMiddle(fact.Key, keyWidth)
		value := lipgloss.Wrap(fact.Value, valueWidth, "")
		for i, line := range strings.Split(value, "\n") {
			prefix := indent
			if i == 0 {
				prefix = style.Key(key) + strings.Repeat(" ", keyWidth-ansi.StringWidth(key)+2)
			}
			if output.Len() > 0 {
				output.WriteByte('\n')
			}
			output.WriteString(strings.TrimRight(prefix+line, " \t\r"))
		}
	}
	return output.String()
}
