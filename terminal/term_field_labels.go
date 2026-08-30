package terminal

import (
	"fmt"
	"strings"

	"github.com/mclucy/lucy/terminal/style"
)

// FieldLabels renders labels as a comma-separated list with line wrapping.
type FieldLabels struct {
	Title          string
	Labels         []string
	MaxWidth       int
	MaxLines       int
	availableWidth int
}

func (f *FieldLabels) KeyLength() int {
	return len(f.Title)
}

func (f *FieldLabels) Render() string {
	key, value := f.RenderRow()
	return renderRow(key, value)
}

func (f *FieldLabels) RenderRow() (key, value string) {
	if len(f.Labels) == 0 {
		return "", ""
	}

	var sb strings.Builder
	maxW := f.maxWidth()
	width := 0
	lines := 1
	for i, label := range f.Labels {
		sb.WriteString(label)
		if i != len(f.Labels)-1 {
			sb.WriteString(", ")
		}
		width += len(label) + 2
		if width >= maxW && i != len(f.Labels)-1 {
			sb.WriteString("\n")
			width = 0
			lines++
			if f.MaxLines != 0 && lines > f.MaxLines {
				sb.WriteString(renderDim(fmt.Sprintf(
					"(%d more, use --long to show all)",
					len(f.Labels)-i-1,
				)))
				return renderKeyStyled(f.Title), sb.String()
			}
		}
	}

	return renderKeyStyled(f.Title), sb.String()
}

func (f *FieldLabels) SetAvailableWidth(width int) {
	f.availableWidth = width
}

func (f *FieldLabels) maxWidth() int {
	if f.MaxWidth != 0 {
		return f.MaxWidth
	}
	if f.availableWidth > 0 {
		return f.availableWidth
	}
	return max(33*style.TermWidth()/100, 40)
}

// FieldDynamicColumnLabels renders labels in a grid sized to the terminal.
type FieldDynamicColumnLabels struct {
	Title          string
	Labels         []string
	MaxLines       int
	MaxColumns     int
	ShowTotal      bool
	NoTitle        bool
	availableWidth int
}

func (f *FieldDynamicColumnLabels) KeyLength() int {
	if f.NoTitle {
		return 0
	}
	return len(f.Title)
}

func (f *FieldDynamicColumnLabels) Render() string {
	key, value := f.RenderRow()
	return renderRow(key, value)
}

func (f *FieldDynamicColumnLabels) RenderRow() (key, value string) {
	if len(f.Labels) == 0 {
		return "", ""
	}

	var sb strings.Builder
	longestLabel := 0
	for _, label := range f.Labels {
		if len(label) > longestLabel {
			longestLabel = len(label)
		}
	}

	colWidth := longestLabel + 2
	availableWidth := f.availableWidth
	if availableWidth <= 0 {
		availableWidth = style.TermWidth()
		if !f.NoTitle {
			availableWidth -= f.KeyLength() + keyColPadding
		}
	}
	columnNumber := availableWidth / colWidth
	if columnNumber <= 0 {
		columnNumber = 1
	}
	if f.MaxColumns != 0 && columnNumber > f.MaxColumns {
		columnNumber = f.MaxColumns
	}

	currentLine := 1
	for i, label := range f.Labels {
		lastInRow := (i+1)%columnNumber == 0
		lastAmongAll := i == len(f.Labels)-1

		sb.WriteString(label)
		sb.WriteString(strings.Repeat(" ", colWidth-len(label)))

		if f.MaxLines != 0 && currentLine == f.MaxLines && lastInRow {
			sb.WriteString("\n")
			if f.ShowTotal {
				sb.WriteString(renderDim(fmt.Sprintf(
					"(%d in total, %d more)",
					len(f.Labels),
					len(f.Labels)-i-1,
				)))
			} else {
				sb.WriteString(renderDim(fmt.Sprintf("(%d more)", len(f.Labels)-i-1)))
			}
			return f.renderRowKey(), sb.String()
		}

		if lastAmongAll {
			if f.ShowTotal {
				sb.WriteString("\n")
				sb.WriteString(renderDim(fmt.Sprintf("(%d total)", len(f.Labels))))
			}
			return f.renderRowKey(), sb.String()
		}

		if lastInRow {
			sb.WriteString("\n")
			currentLine++
		}
	}

	return f.renderRowKey(), sb.String()
}

func (f *FieldDynamicColumnLabels) SetAvailableWidth(width int) {
	f.availableWidth = width
}

func (f *FieldDynamicColumnLabels) renderRowKey() string {
	if f.NoTitle {
		return ""
	}
	return renderKeyStyled(f.Title)
}
