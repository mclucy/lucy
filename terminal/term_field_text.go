package terminal

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/mclucy/lucy/terminal/style"
)

// FieldShortText renders a key-value pair on one line.
type FieldShortText struct {
	Title string
	Text  string
}

func (f *FieldShortText) KeyLength() int {
	return len(f.Title)
}

func (f *FieldShortText) Render() string {
	key, value := f.RenderRow()
	return renderRow(key, value)
}

func (f *FieldShortText) RenderRow() (key, value string) {
	return renderKeyStyled(f.Title), f.Text
}

// FieldMarkdown renders Markdown content as styled ANSI terminal output.
type FieldMarkdown FieldLongText

func (f *FieldMarkdown) KeyLength() int {
	return len(f.Title)
}

func (f *FieldMarkdown) Render() string {
	key, value := f.RenderRow()
	return renderRow(key, value)
}

func (f *FieldMarkdown) RenderRow() (key, value string) {
	long := FieldLongText(*f)
	long.Text = style.MarkdownToAnsi(f.Text, f.MaxColumns)
	long.LineWrap = false
	return long.RenderRow()
}

func (f *FieldMarkdown) SetAvailableWidth(width int) {
	(*FieldLongText)(f).SetAvailableWidth(width)
}

// FieldLongText renders multi-line text with optional wrapping and truncation.
type FieldLongText struct {
	Title string
	Text  string

	Padding    bool
	LineWrap   bool
	MaxColumns int
	MaxLines   int

	UseAlternate   bool
	AlternateText  string
	FoldNotice     string
	availableWidth int
}

func (f *FieldLongText) KeyLength() int {
	return len(f.Title)
}

func (f *FieldLongText) Render() string {
	key, value := f.RenderRow()
	return renderRow(key, value)
}

func (f *FieldLongText) RenderRow() (key, value string) {
	text := f.Text
	if f.LineWrap {
		text = lipgloss.Wrap(text, f.maxColumns(), "")
	}
	lines := strings.Split(text, "\n")
	lineNumber := len(lines)
	lineNumberAnnotation := renderDim(
		fmt.Sprintf("(total %d lines)", lineNumber),
	)

	truncated := f.MaxLines != 0 && len(lines) > f.MaxLines
	if truncated {
		if f.UseAlternate {
			if f.AlternateText == "" {
				return "", ""
			}
			foldNotice := f.FoldNotice
			if foldNotice == "" {
				foldNotice = "full text not shown, use --long or expand the terminal"
			}
			value := f.AlternateText + " " + lineNumberAnnotation + "\n" + renderDim(foldNotice)
			return renderKeyStyled(f.Title), value
		}

		foldNotice := f.FoldNotice
		if foldNotice == "" {
			foldNotice = fmt.Sprintf(
				"...\n%d lines left, use --long or expand the terminal\n",
				lineNumber-f.MaxLines,
			)
		}
		lines = lines[:f.MaxLines]
		lines = append(lines, renderDim(foldNotice))
	}

	var sb strings.Builder
	sb.WriteString(lineNumberAnnotation)
	sb.WriteString("\n")
	if f.Padding {
		sb.WriteString(renderSeparator(5, false))
	}
	for _, line := range lines {
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	return renderKeyStyled(f.Title), strings.TrimSuffix(sb.String(), "\n")
}

func (f *FieldLongText) SetAvailableWidth(width int) {
	f.availableWidth = width
}

func (f *FieldLongText) maxColumns() int {
	if f.availableWidth > 0 && (f.MaxColumns == 0 || f.availableWidth < f.MaxColumns) {
		return f.availableWidth
	}
	return f.MaxColumns
}

// FieldAnnotatedShortText renders a key-value pair with an inline annotation.
type FieldAnnotatedShortText struct {
	Title      string
	Text       string
	Annotation string
}

func (f *FieldAnnotatedShortText) KeyLength() int {
	return len(f.Title)
}

func (f *FieldAnnotatedShortText) Render() string {
	key, value := f.RenderRow()
	return renderRow(key, value)
}

func (f *FieldAnnotatedShortText) RenderRow() (key, value string) {
	var sb strings.Builder
	sb.WriteString(f.Text)
	if f.Annotation != "" {
		sb.WriteString(renderAnnot(f.Annotation))
	}
	return renderKeyStyled(f.Title), sb.String()
}
