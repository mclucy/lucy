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

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"charm.land/lipgloss/v2/tree"
	"github.com/mclucy/lucy/tui/style"
	"golang.org/x/term"
)

// Data is a collection of Field values to be rendered together.
type Data struct {
	Fields []Field
}

// Field is the interface for all renderable output elements. Each
// implementation returns its formatted string representation from Render.
type Field interface {
	Render() string
	RenderRow() (key, value string)
	KeyLength() int
}

// WidthAware is implemented by fields that need the value-column width before
// rendering multi-line or grid content.
type WidthAware interface {
	SetAvailableWidth(width int)
}

type segment struct {
	rows       []tableRow
	standalone string
}

type tableRow struct {
	key   string
	value string
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
	_, value := f.RenderRow()
	return value
}

func (f *FieldSeparator) RenderRow() (key, value string) {
	if f.Proportional {
		f.Length = f.Length * style.TermWidth() / 100
	}
	if f.Length == 0 {
		f.Length = style.TermWidth() * 8 / 10
	}
	return "", renderSeparator(f.Length, f.Dim)
}

var borderChar = lipgloss.NormalBorder().Bottom

// FieldAnnotation renders a single line of dimmed annotation text.
type FieldAnnotation struct {
	Annotation string
}

func (f *FieldAnnotation) KeyLength() int {
	return 0
}

func (f *FieldAnnotation) Render() string {
	_, value := f.RenderRow()
	return value
}

func (f *FieldAnnotation) RenderRow() (key, value string) {
	return "", renderDim(f.Annotation) + "\n"
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

// FieldLongText renders multi-line text content with optional word-wrapping
// and line count truncation.
type FieldLongText struct {
	Title string
	Text  string

	Padding    bool // Padding adds a short separator before the text body
	LineWrap   bool
	MaxColumns int
	MaxLines   int

	UseAlternate   bool   // UseAlternate shows AlternateText instead of the text body if it is truncated
	AlternateText  string // AlternateText is shown instead of the text body if it is truncated
	FoldNotice     string // FoldNotice is a dimmed message shown after the text body if it is truncated, left empty for default message
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

type FieldTree struct {
	Title      string
	Text       string
	Annotation string
	Children   []TreeNode
}

type TreeNode struct {
	Title string
	Field Field
}

func (f *FieldTree) KeyLength() int {
	maxLength := len(f.Title)
	for _, child := range f.Children {
		if length := len(child.Title); length > maxLength {
			maxLength = length
		}
	}
	return maxLength
}

func (f *FieldTree) Render() string {
	key, value := f.RenderRow()
	out := renderRow(key, value)
	if children := f.RenderChildren(); children != "" {
		out += children
	}
	return out
}

// RenderRow returns the parent row only (key + value + annotation), without
// any children. Children are rendered separately via RenderChildren so that
// the tree can be emitted as a standalone segment aligned at column 0,
// matching the parent's key column instead of the value column.
func (f *FieldTree) RenderRow() (key, value string) {
	var sb strings.Builder
	sb.WriteString(f.Text)
	if f.Annotation != "" {
		sb.WriteString(renderAnnot(f.Annotation))
	}
	return renderKeyStyled(f.Title), sb.String()
}

// RenderChildren renders the children subtree as a standalone string aligned
// at column 0. Returns an empty string when there are no children.
func (f *FieldTree) RenderChildren() string {
	if len(f.Children) == 0 {
		return ""
	}
	childKeyWidth := f.childKeyWidth()
	t := tree.New()
	for _, child := range f.Children {
		if child.Field == nil {
			continue
		}
		label := treeChildLabel(child, childKeyWidth)
		t.Child(label)
	}
	return t.String()
}

// HasChildren reports whether this field has child nodes to render as a
// subtree. Used by buildSegments to decide whether to emit a standalone
// tree segment after the parent row.
func (f *FieldTree) HasChildren() bool {
	return len(f.Children) > 0
}

func (f *FieldTree) childKeyWidth() int {
	width := 0
	for _, child := range f.Children {
		if child.Field == nil {
			continue
		}
		if length := len(child.Title); length > width {
			width = length
		}
	}
	return width + keyColPadding
}

func treeChildLabel(node TreeNode, keyWidth int) string {
	key := renderKeyFixed(node.Title, keyWidth)
	switch field := node.Field.(type) {
	case *FieldShortText:
		return key + field.Text
	case *FieldAnnotatedShortText:
		if field.Annotation != "" {
			return key + field.Text + renderAnnot(field.Annotation)
		}
		return key + field.Text
	case *FieldDynamicColumnLabels:
		if widthAware, ok := any(field).(WidthAware); ok {
			widthAware.SetAvailableWidth(style.TermWidth() - keyWidth)
		}
		_, value := field.RenderRow()
		value = strings.TrimSuffix(value, "\n")
		if value == "" {
			return key
		}
		return key + value
	case *FieldMultiAnnotatedShortText:
		_, value := field.RenderRow()
		value = strings.TrimSuffix(value, "\n")
		if value == "" {
			return key
		}
		return key + value
	case *FieldMultiShortText:
		if len(field.Texts) > 0 {
			return key + field.Texts[0]
		}
		return key
	default:
		return strings.TrimSuffix(node.Field.Render(), "\n")
	}
}

// FieldNil is a no-op field that renders nothing.
var FieldNil = &fieldNil{}

type fieldNil struct{}

func (f *fieldNil) KeyLength() int {
	return 0
}

func (f *fieldNil) Render() string { return "" }

func (f *fieldNil) RenderRow() (key, value string) { return "", "" }

// FieldLabels renders a title followed by a comma-separated list of labels
// that wraps across lines. If MaxWidth is 0, it defaults to
// max(33% of terminal width, 40).
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
				sb.WriteString(renderDim("(" + strconv.Itoa(len(f.Labels)-i-1) + " more, use --long to show all)"))
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

// FieldDynamicColumnLabels renders labels in a dynamic grid whose column
// count is derived from the terminal width and longest label length.
//
// NoTitle renders a label-only grid without a key column, useful for search
// results and similar content.
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

		padded := label + strings.Repeat(" ", colWidth-len(label))
		sb.WriteString(padded)

		// If MaxLines is set, and we've reached the limit, show a total count of
		// remaining labels and stop rendering more.
		if f.MaxLines != 0 && currentLine == f.MaxLines && lastInRow {
			sb.WriteString("\n")
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
			return f.renderRowKey(), sb.String()
		}

		// If this is the last label, optionally show a total count of all labels.
		if lastAmongAll {
			if f.ShowTotal {
				sb.WriteString("\n")
				sb.WriteString(
					renderDim(
						fmt.Sprintf(
							"(%d total)",
							len(f.Labels),
						),
					),
				)
			}
			return f.renderRowKey(), sb.String()
		}

		// For the last label in a row, add a newline and indentation for the next row.
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
	key, value := f.RenderRow()
	return renderRow(key, value)
}

func (f *FieldMultiAnnotatedShortText) RenderRow() (key, value string) {
	if len(f.Texts) == 0 {
		return "", ""
	}

	var sb strings.Builder
	for i, t := range f.Texts {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(t)
		if f.Annotations != nil && i < len(f.Annotations) {
			sb.WriteString(renderAnnot(f.Annotations[i]))
		}
	}

	if f.ShowTotal {
		sb.WriteString("\n")
		sb.WriteString(renderDim("(" + strconv.Itoa(len(f.Texts)) + " total)"))
	}

	return renderKeyStyled(f.Title), sb.String()
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
	key, value := f.RenderRow()
	return renderRow(key, value)
}

func (f *FieldMultiShortText) RenderRow() (key, value string) {
	if len(f.Texts) == 0 {
		return "", ""
	}

	var sb strings.Builder
	for i, t := range f.Texts {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(t)
	}

	if f.ShowTotal {
		sb.WriteString("\n")
		sb.WriteString(renderDim("(" + strconv.Itoa(len(f.Texts)) + " total)"))
	}

	return renderKeyStyled(f.Title), sb.String()
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
	key, value := f.RenderRow()
	return renderRow(key, value)
}

func (f *FieldCheckBox) RenderRow() (key, value string) {
	trueText := f.TrueText
	if trueText == "" {
		trueText = style.Success("\u2713") // ✓
	}
	falseText := f.FalseText
	if falseText == "" {
		falseText = style.Failure("\u2717") // ✗
	}

	if f.Boolean {
		return renderKeyStyled(f.Title), trueText
	}
	return renderKeyStyled(f.Title), falseText
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
		keyWidth := keyColumnWidth(data.Fields)
		infoBlock := renderSegments(buildSegments(data.Fields, 0, keyWidth), 0, keyWidth)
		fmt.Println(infoBlock)
		return
	}

	isTTY := term.IsTerminal(1)
	params := NegotiateStatusLayout(
		style.TermWidth(),
		logoField.Width(LogoLargePlain),
		logoField.Width(LogoSmallPlain),
		isTTY,
	)
	infoFields := fieldsWithoutLogo(data.Fields)
	keyWidth := keyColumnWidth(infoFields)
	infoBlock := renderSegments(buildSegments(infoFields, params.InfoWidth, keyWidth), params.InfoWidth, keyWidth)

	var output string
	switch params.Mode {
	case LayoutLargeLogoSideBySide, LayoutSmallLogoSideBySide:
		variant := LogoLargePlain
		if params.Mode == LayoutSmallLogoSideBySide {
			variant = LogoSmallPlain
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
		logoLines := logoField.Lines(LogoLargePlain)
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
