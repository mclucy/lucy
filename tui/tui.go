// Package tui is a key-value based commandline output framework.
//
// The core of this package is the Data struct, which holds an array of Field
// values representing different types of output formats. Each Field implements
// the Render() method that returns a formatted string. The Data struct can be
// passed to Flush to print the composed output.
//
// Rendering uses lipgloss-based styling instead of raw ANSI codes, and
// fixed-width key columns instead of tabwriter for simpler, more predictable
// layout.
//
// Note: a field will not show if its content is empty.
package tui

import (
	"fmt"
	"strconv"
	"strings"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/muesli/reflow/wrap"
	"golang.org/x/term"

	"github.com/mclucy/lucy/tools"
)

// Data is a collection of Field values to be rendered together.
type Data struct {
	Fields []Field
}

// Field is the interface for all renderable output elements. Each
// implementation returns its formatted string representation from Render.
type Field interface {
	Render() string
	KeyLength() int
}

// FieldSeparator renders a horizontal separator line. A Length of 0 produces
// a line spanning 80% of the terminal width.
//
// Proportional turns the Length value into a percentage of the terminal width
// instead of a character count, so Length=50 with Proportional=true would render
// a line spanning 50% of the terminal width. If Proportional is true, Length
// is treated as a percentage and should be between 0 and 100; values outside
// this range will be clamped.
type FieldSeparator struct {
	Length       int
	Proportional bool
	Dim          bool
}

func (f *FieldSeparator) KeyLength() int {
	return 0
}

func (f *FieldSeparator) Render() string {
	if f.Proportional {
		f.Length = f.Length * tools.TermWidth() / 100
	}
	if f.Length == 0 {
		f.Length = tools.TermWidth() * 8 / 10
	}
	return renderSeparator(f.Length, f.Dim)
}

// FieldAnnotation renders a single line of dimmed annotation text.
type FieldAnnotation struct {
	Annotation string
}

func (f *FieldAnnotation) KeyLength() int {
	return 0
}

func (f *FieldAnnotation) Render() string {
	return renderDim(f.Annotation) + "\n"
}

// FieldShortText renders a key-value pair on one line.
type FieldShortText struct {
	Title string
	Text  string
}

func (f *FieldShortText) KeyLength() int {
	return len(f.Title)
}

func (f *FieldShortText) Render() string {
	return renderKey(f.Title) + f.Text + "\n"
}

// FieldMarkdown renders Markdown content as styled ANSI terminal output.
type FieldMarkdown FieldLongText

func (f *FieldMarkdown) KeyLength() int {
	return len(f.Title)
}

func (f *FieldMarkdown) Render() string {
	long := FieldLongText(*f)
	long.Text = tools.MarkdownToAnsi(f.Text, f.MaxColumns)
	long.LineWrap = false
	return long.Render() + "\n"
}

// FieldLongText renders multi-line text content with optional word-wrapping
// and line count truncation.
type FieldLongText struct {
	Title string
	Text  string

	Padding    bool // Padding adds a short separator before the text body
	LineWrap   bool
	MaxColumns int
	MaxLines   int

	UseAlternate  bool   // UseAlternate shows AlternateText instead of the text body if it is truncated
	AlternateText string // AlternateText is shown instead of the text body if it is truncated
	FoldNotice    string // FoldNotice is a dimmed message shown after the text body if it is truncated, left empty for default message
}

func (f *FieldLongText) KeyLength() int {
	return len(f.Title)
}

func (f *FieldLongText) Render() string {
	text := f.Text
	if f.LineWrap {
		text = wrap.String(text, f.MaxColumns)
	}
	lines := strings.Split(text, "\n")
	lineNumber := len(lines)
	// lineNumberAnnotation shows the full line count, regardless of truncation.
	lineNumberAnnotation := renderDim(
		fmt.Sprintf("(total %d lines)", lineNumber),
	)

	// If MaxLines is set and the text exceeds it, truncate or show alternate text.
	truncated := f.MaxLines != 0 && len(lines) > f.MaxLines
	if truncated {
		// If UseAlternate is true, show AlternateText instead of the truncated text body.
		if f.UseAlternate {
			if f.AlternateText == "" {
				return ""
			}
			alternateText := FieldShortText{
				Title: f.Title,
				Text:  f.AlternateText + " " + lineNumberAnnotation,
			}
			rendered := alternateText.Render()

			// Use default fold notice if FoldNotice is empty
			if f.FoldNotice == "" {
				f.FoldNotice = renderDim(fmt.Sprintf("full text not shown, use --long or expand the terminal"))
			}
			rendered += renderTab() + renderDim(f.FoldNotice)
			return rendered
		}

		// Use default fold notice if FoldNotice is empty
		if f.FoldNotice == "" {
			f.FoldNotice = fmt.Sprintf(
				"...\n%d lines left, use --long or expand the terminal\n",
				lineNumber-f.MaxLines,
			)
		}
		f.FoldNotice = renderDim(f.FoldNotice)
		// Truncate to MaxLines
		lines = lines[:f.MaxLines]
		// Append fold notice after truncated text
		lines = append(lines, f.FoldNotice)
	}

	var sb strings.Builder
	sb.WriteString(renderKey(f.Title))
	sb.WriteString(lineNumberAnnotation)
	sb.WriteString("\n")
	if f.Padding {
		sb.WriteString(renderSeparator(5, false))
	}
	for _, line := range lines {
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	return sb.String()
}

// FieldAnnotatedShortText renders a key-value pair with a dimmed annotation
// placed inline after the value.
type FieldAnnotatedShortText struct {
	Title      string
	Text       string
	Annotation string
}

func (f *FieldAnnotatedShortText) KeyLength() int {
	return len(f.Title)
}

func (f *FieldAnnotatedShortText) Render() string {
	var sb strings.Builder
	sb.WriteString(renderKey(f.Title))
	sb.WriteString(f.Text)
	if f.Annotation != "" {
		sb.WriteString(renderAnnot(f.Annotation))
	}
	sb.WriteString("\n")
	return sb.String()
}

// FieldNil is a no-op field that renders nothing.
var FieldNil = &fieldNil{}

type fieldNil struct{}

func (f *fieldNil) KeyLength() int {
	return 0
}

func (f *fieldNil) Render() string { return "" }

// FieldLabels renders a title followed by a comma-separated list of labels
// that wraps across lines. If MaxWidth is 0, it defaults to
// max(33% of terminal width, 40).
type FieldLabels struct {
	Title    string
	Labels   []string
	MaxWidth int
	MaxLines int
}

func (f *FieldLabels) KeyLength() int {
	return len(f.Title)
}

func (f *FieldLabels) Render() string {
	if len(f.Labels) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(renderKey(f.Title))

	maxW := f.MaxWidth
	if maxW == 0 {
		maxW = max(33*tools.TermWidth()/100, 40)
	}

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
			sb.WriteString(renderTab())
			width = 0
			lines++
			if f.MaxLines != 0 && lines > f.MaxLines {
				sb.WriteString(renderDim("(" + strconv.Itoa(len(f.Labels)-i-1) + " more, use --long to show all)"))
				sb.WriteString("\n")
				return sb.String()
			}
		}
	}

	if width != 0 {
		sb.WriteString("\n")
	}

	return sb.String()
}

// FieldDynamicColumnLabels renders labels in a dynamic grid whose column
// count is derived from the terminal width and longest label length.
//
// NoTitle renders a label-only grid without a key column, useful for search
// results and similar content.
type FieldDynamicColumnLabels struct {
	Title      string
	Labels     []string
	MaxLines   int
	MaxColumns int
	ShowTotal  bool
	NoTitle    bool
}

func (f *FieldDynamicColumnLabels) KeyLength() int {
	return len(f.Title)
}

func (f *FieldDynamicColumnLabels) Render() string {
	if len(f.Labels) == 0 {
		return ""
	}

	var sb strings.Builder

	if !f.NoTitle {
		sb.WriteString(renderKey(f.Title))
	}

	longestLabel := 0
	for _, label := range f.Labels {
		if len(label) > longestLabel {
			longestLabel = len(label)
		}
	}

	colWidth := longestLabel + 2
	columnNumber := (tools.TermWidth() - keyColumnWidth) / colWidth
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

		padded := label + strings.Repeat(" ", colWidth-len(label))
		sb.WriteString(padded)

		// If MaxLines is set, and we've reached the limit, show a total count of
		// remaining labels and stop rendering more.
		if f.MaxLines != 0 && currentLine == f.MaxLines && lastInRow {
			sb.WriteString("\n")
			if !f.NoTitle {
				sb.WriteString(renderTab())
			}
			if f.ShowTotal {
				sb.WriteString(
					renderDim(
						fmt.Sprintf(
							"(%d in total, %d more)",
							len(f.Labels),
							len(f.Labels)-i-1,
						),
					),
				)
			} else {
				sb.WriteString(
					renderDim(
						fmt.Sprintf(
							"(%d more)",
							len(f.Labels)-i-1,
						),
					),
				)
			}
			sb.WriteString("\n")
			return sb.String()
		}

		// If this is the last label, optionally show a total count of all labels.
		if lastAmongAll {
			if f.ShowTotal {
				sb.WriteString("\n")
				if lastInRow && !f.NoTitle {
					sb.WriteString(renderTab())
				}
				sb.WriteString(
					renderDim(
						fmt.Sprintf(
							"(%d total)",
							len(f.Labels),
						),
					),
				)
			}
			sb.WriteString("\n")
			return sb.String()
		}

		// For the last label in a row, add a newline and indentation for the next row.
		if lastInRow {
			sb.WriteString("\n")
			currentLine++
			if !f.NoTitle {
				sb.WriteString(renderTab())
			}
		}
	}

	return sb.String()
}

// FieldMultiAnnotatedShortText renders multiple annotated lines under one key.
// len(Texts) determines the number of lines; extra entries in Annotations are ignored.
type FieldMultiAnnotatedShortText struct {
	Title       string
	Texts       []string
	Annotations []string
	ShowTotal   bool
}

func (f *FieldMultiAnnotatedShortText) KeyLength() int {
	return len(f.Title)
}

func (f *FieldMultiAnnotatedShortText) Render() string {
	if len(f.Texts) == 0 {
		return ""
	}

	var sb strings.Builder
	for i, t := range f.Texts {
		if i == 0 {
			sb.WriteString(renderKey(f.Title))
		} else {
			sb.WriteString(renderTab())
		}
		sb.WriteString(t)
		if f.Annotations != nil && i < len(f.Annotations) {
			sb.WriteString(renderAnnot(f.Annotations[i]))
		}
		sb.WriteString("\n")
	}

	if f.ShowTotal {
		sb.WriteString(renderTab())
		sb.WriteString(renderDim("(" + strconv.Itoa(len(f.Texts)) + " total)"))
		sb.WriteString("\n")
	}

	return sb.String()
}

// FieldMultiShortText renders multiple values under a single key, one per line.
type FieldMultiShortText struct {
	Title     string
	Texts     []string
	ShowTotal bool
}

func (f *FieldMultiShortText) KeyLength() int {
	return len(f.Title)
}

func (f *FieldMultiShortText) Render() string {
	if len(f.Texts) == 0 {
		return ""
	}

	var sb strings.Builder
	for i, t := range f.Texts {
		if i == 0 {
			sb.WriteString(renderKey(f.Title))
		} else {
			sb.WriteString(renderTab())
		}
		sb.WriteString(t)
		sb.WriteString("\n")
	}

	if f.ShowTotal {
		sb.WriteString(renderTab())
		sb.WriteString(renderDim("(" + strconv.Itoa(len(f.Texts)) + " total)"))
		sb.WriteString("\n")
	}

	return sb.String()
}

// FieldCheckBox renders a boolean value as a check mark (✓) or cross (✗).
// Custom TrueText/FalseText override the defaults.
type FieldCheckBox struct {
	Title     string
	Boolean   bool
	TrueText  string
	FalseText string
}

func (f *FieldCheckBox) KeyLength() int {
	return len(f.Title)
}

func (f *FieldCheckBox) Render() string {
	trueText := f.TrueText
	if trueText == "" {
		trueText = tools.Green("\u2713") // ✓
	}
	falseText := f.FalseText
	if falseText == "" {
		falseText = tools.Red("\u2717") // ✗
	}

	var sb strings.Builder
	sb.WriteString(renderKey(f.Title))
	if f.Boolean {
		sb.WriteString(trueText)
	} else {
		sb.WriteString(falseText)
	}
	sb.WriteString("\n")
	return sb.String()
}

// clipLines truncates each line in s to at most maxWidth runes.
// A maxWidth <= 0 is treated as "no clipping".
func clipLines(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		runes := []rune(l)
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
		for _, field := range data.Fields {
			if field.KeyLength() > keyColumnWidth {
				keyColumnWidth = field.KeyLength()
			}
		}
		keyColumnWidth += keyColPadding

		var sb strings.Builder
		for _, field := range data.Fields {
			if field != nil {
				sb.WriteString(field.Render())
			}
		}
		sb.WriteString("\n")
		fmt.Print(sb.String())
		return
	}

	// LOGO BRANCH: uses a local key width; does not corrupt the global.

	localKeyWidth := 0
	for _, field := range data.Fields {
		if _, ok := field.(*FieldLogo); ok {
			continue
		}
		if field.KeyLength() > localKeyWidth {
			localKeyWidth = field.KeyLength()
		}
	}
	localKeyWidth += keyColPadding

	// Save/restore keyColumnWidth so existing Render() methods pick up our
	// local width without permanently corrupting the global.
	savedKeyColumnWidth := keyColumnWidth
	keyColumnWidth = localKeyWidth
	defer func() { keyColumnWidth = savedKeyColumnWidth }()
	var infoSb strings.Builder
	for _, field := range data.Fields {
		if field == nil {
			continue
		}
		if _, ok := field.(*FieldLogo); ok {
			continue
		}
		infoSb.WriteString(field.Render())
	}
	infoBlock := infoSb.String()

	isTTY := term.IsTerminal(int(1))
	params := NegotiateStatusLayout(
		tools.TermWidth(),
		logoField.Width(LogoLarge),
		logoField.Width(LogoSmall),
		isTTY,
	)

	var output string
	switch params.Mode {
	case LayoutLargeLogoSideBySide, LayoutSmallLogoSideBySide:
		variant := LogoLarge
		if params.Mode == LayoutSmallLogoSideBySide {
			variant = LogoSmall
		}
		logoLines := logoField.Lines(variant)
		logoBlock := strings.Join(logoLines, "\n")
		gapStr := strings.Repeat(" ", params.GapWidth)
		infoStyle := lipgloss.NewStyle().Width(params.InfoWidth)
		constrainedInfo := infoStyle.Render(infoBlock)
		output = lipgloss.JoinHorizontal(lipgloss.Top, logoBlock, gapStr, constrainedInfo)

	case LayoutVertical:
		logoLines := logoField.Lines(LogoLarge)
		output = strings.Join(logoLines, "\n") + "\n\n" + infoBlock

	case LayoutClipped:
		output = clipLines(infoBlock, params.InfoWidth)

	default:
		output = infoBlock
	}
	fmt.Print(output)
	fmt.Println()
}
