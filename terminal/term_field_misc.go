package terminal

import (
	"charm.land/lipgloss/v2"
	"github.com/mclucy/lucy/terminal/style"
)

// FieldSeparator renders a horizontal separator line. A Length of 0 produces
// a line spanning 80% of the terminal width.
//
// Proportional turns Length into a percentage of the terminal width.
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

// FieldAnnotation renders one line of dimmed annotation text.
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

// FieldNil is a no-op field that renders nothing.
var FieldNil = &fieldNil{}

type fieldNil struct{}

func (f *fieldNil) KeyLength() int {
	return 0
}

func (f *fieldNil) Render() string { return "" }

func (f *fieldNil) RenderRow() (key, value string) { return "", "" }
