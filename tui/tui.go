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

	"github.com/muesli/reflow/wrap"

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
}

// FieldSeparator renders a horizontal separator line. A Length of 0 produces
// a line spanning 75% of the terminal width.
type FieldSeparator struct {
	Length int
	Dim    bool
}

func (f *FieldSeparator) Render() string {
	return renderSeparator(f.Length, f.Dim)
}

// FieldAnnotation renders a single line of dimmed annotation text.
type FieldAnnotation struct {
	Annotation string
}

func (f *FieldAnnotation) Render() string {
	return renderDim(f.Annotation) + "\n"
}

// FieldShortText renders a key-value pair on one line.
type FieldShortText struct {
	Title string
	Text  string
}

func (f *FieldShortText) Render() string {
	return renderKey(f.Title) + f.Text + "\n"
}

// FieldMarkdown renders Markdown content as styled ANSI terminal output.
type FieldMarkdown FieldLongText

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
type FieldDynamicColumnLabels struct {
	Title     string
	Labels    []string
	MaxLines  int
	ShowTotal bool
}

func (f *FieldDynamicColumnLabels) Render() string {
	if len(f.Labels) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(renderKey(f.Title))

	maxLabelLen := 0
	for _, label := range f.Labels {
		if len(label) > maxLabelLen {
			maxLabelLen = len(label)
		}
	}

	colWidth := maxLabelLen + 2
	columns := (tools.TermWidth() - keyColumnWidth) / colWidth
	if columns <= 0 {
		columns = 1
	}

	lines := 1
	for i, label := range f.Labels {
		lastInRow := (i+1)%columns == 0
		lastAmongAll := i == len(f.Labels)-1

		padded := label + strings.Repeat(" ", colWidth-len(label))
		sb.WriteString(padded)

		if f.MaxLines != 0 && lines == f.MaxLines && lastInRow {
			sb.WriteString("\n")
			sb.WriteString(renderTab())
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

		if lastAmongAll {
			if f.ShowTotal {
				if lastInRow {
					sb.WriteString("\n")
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

		if lastInRow {
			sb.WriteString("\n")
			lines++
			sb.WriteString(renderTab())
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

// Flush renders all fields in data and prints the composed output.
func Flush(data *Data) {
	var sb strings.Builder
	for _, field := range data.Fields {
		if field != nil {
			sb.WriteString(field.Render())
		}
	}
	sb.WriteString("\n")
	fmt.Print(sb.String())
}
