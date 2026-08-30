package terminal

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/mclucy/lucy/terminal/style"
	"golang.org/x/term"
)

const keyColPadding = 2

func renderKeyStyled(title string) string {
	return style.Key(title)
}

func renderKeyFixed(title string, width int) string {
	styled := style.Key(title)
	visualWidth := lipgloss.Width(styled)
	padding := max(width-visualWidth, keyColPadding)
	return styled + strings.Repeat(" ", padding)
}

func renderDim(text string) string {
	return style.Muted(text)
}

func renderAnnot(annotation string) string {
	return "  " + style.Muted(annotation)
}

func renderSeparator(length int, dim bool) string {
	if length > style.TermWidth() {
		length = style.TermWidth()
	}
	sep := lipgloss.NewStyle().
		Faint(dim).
		Render(strings.Repeat(borderChar, length))
	return sep + "\n"
}

func (f *FieldLogo) RenderRow() (key, value string) {
	return "", f.Render()
}

func renderRow(key, value string) string {
	if key == "" && value == "" {
		return ""
	}
	return key + value + "\n"
}

// clipLines truncates each line in s to maxWidth runes.
func clipLines(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		runes := []rune(line)
		if len(runes) > maxWidth {
			lines[i] = string(runes[:maxWidth])
		}
	}
	return strings.Join(lines, "\n")
}

// Flush renders all fields in data and prints the composed output.
func Flush(data *Data) {
	var logoField *FieldLogo
	for _, field := range data.Fields {
		if fl, ok := field.(*FieldLogo); ok {
			logoField = fl
			break
		}
	}

	if logoField == nil {
		keyWidth := keyColumnWidth(data.Fields)
		infoBlock := renderSegments(buildSegments(data.Fields, 0, keyWidth), 0, keyWidth)
		fmt.Println(infoBlock)
		return
	}

	logoMode := data.LogoMode
	largeVariant := logoField.variant(true)
	smallVariant := logoField.variant(false)
	isTTY := term.IsTerminal(1)
	params := NegotiateStatusLayout(
		style.TermWidth(),
		logoField.Width(largeVariant),
		logoField.Width(smallVariant),
		isTTY,
		logoMode,
	)
	infoFields := fieldsWithoutLogo(data.Fields)
	keyWidth := keyColumnWidth(infoFields)
	infoBlock := renderSegments(
		buildSegments(infoFields, params.InfoWidth, keyWidth),
		params.InfoWidth,
		keyWidth,
	)

	var output string
	switch params.Mode {
	case LayoutLargeLogoSideBySide, LayoutSmallLogoSideBySide:
		variant := largeVariant
		if params.Mode == LayoutSmallLogoSideBySide {
			variant = smallVariant
		}
		logoLines := logoField.Lines(variant)
		logoBlock := strings.Join(logoLines, "\n")
		gapStr := strings.Repeat(" ", params.GapWidth)
		infoStyle := lipgloss.NewStyle().Width(params.InfoWidth)
		constrainedInfo := infoStyle.Render(infoBlock)
		output = lipgloss.JoinHorizontal(
			lipgloss.Top,
			logoBlock,
			gapStr,
			constrainedInfo,
		)

	case LayoutVertical:
		variant := largeVariant
		if logoMode == StatusLogoSmall {
			variant = smallVariant
		}
		logoLines := logoField.Lines(variant)
		output = strings.Join(logoLines, "\n") + "\n\n" + infoBlock

	case LayoutClipped:
		output = clipLines(infoBlock, params.InfoWidth)

	default:
		output = infoBlock
	}
	fmt.Print(output)
	fmt.Println()
}

func fieldsWithoutLogo(fields []Field) []Field {
	filtered := make([]Field, 0, len(fields))
	for _, field := range fields {
		if field == nil {
			continue
		}
		if _, ok := field.(*FieldLogo); ok {
			continue
		}
		filtered = append(filtered, field)
	}
	return filtered
}

func keyColumnWidth(fields []Field) int {
	width := 0
	for _, field := range fields {
		if field == nil {
			continue
		}
		if length := field.KeyLength(); length > width {
			width = length
		}
	}
	if width == 0 {
		return 0
	}
	return width + keyColPadding
}

func buildSegments(fields []Field, totalWidth int, keyWidth int) []segment {
	segments := []segment{}
	rows := []tableRow{}
	flushRows := func() {
		if len(rows) == 0 {
			return
		}
		segments = append(segments, segment{rows: rows})
		rows = []tableRow{}
	}

	for _, field := range fields {
		if field == nil {
			continue
		}
		switch typed := field.(type) {
		case *FieldSeparator:
			flushRows()
			_, value := typed.RenderRow()
			if value != "" {
				segments = append(segments, segment{standalone: value})
			}
			continue
		case *FieldAnnotation:
			flushRows()
			_, value := typed.RenderRow()
			if value != "" {
				segments = append(segments, segment{standalone: value})
			}
			continue
		case *FieldTree:
			if widthAware, ok := any(typed).(WidthAware); ok {
				widthAware.SetAvailableWidth(valueColumnWidth(totalWidth, keyWidth))
			}
			if typed.HasChildren() {
				key, value := typed.RenderRow()
				rows = append(rows, tableRow{key: key, value: value})
				flushRows()
				if children := typed.RenderChildren(); children != "" {
					segments = append(segments, segment{standalone: children})
				}
				continue
			}
		}

		if widthAware, ok := field.(WidthAware); ok {
			widthAware.SetAvailableWidth(valueColumnWidth(totalWidth, keyWidth))
		}
		key, value := field.RenderRow()
		if key == "" && value == "" {
			continue
		}
		rows = append(rows, tableRow{key: key, value: value})
	}
	flushRows()
	return segments
}

func valueColumnWidth(totalWidth int, keyWidth int) int {
	if totalWidth <= 0 {
		totalWidth = style.TermWidth()
	}
	width := totalWidth - keyWidth
	if width <= 0 {
		return 1
	}
	return width
}

func renderSegments(segments []segment, totalWidth int, keyWidth int) string {
	parts := make([]string, 0, len(segments))
	for _, segment := range segments {
		var rendered string
		if len(segment.rows) > 0 {
			rendered = buildTable(segment.rows, totalWidth, keyWidth)
		} else {
			rendered = segment.standalone
		}
		rendered = strings.TrimSuffix(rendered, "\n")
		if rendered != "" {
			parts = append(parts, rendered)
		}
	}
	return strings.Join(parts, "\n")
}

func buildTable(rows []tableRow, totalWidth int, keyWidth int) string {
	t := table.New().
		Border(lipgloss.HiddenBorder()).
		BorderTop(false).
		BorderBottom(false).
		BorderLeft(false).
		BorderRight(false).
		BorderColumn(false).
		BorderRow(false).
		BorderHeader(false).
		StyleFunc(func(row, col int) lipgloss.Style {
			if col == 0 && keyWidth > 0 {
				return lipgloss.NewStyle().Width(keyWidth)
			}
			return lipgloss.NewStyle()
		})

	for _, row := range rows {
		t.Row(row.key, row.value)
	}

	if totalWidth > 0 {
		t.Width(totalWidth)
	}

	return t.String()
}
