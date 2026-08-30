package terminal

import (
	"strconv"
	"strings"

	"github.com/mclucy/lucy/terminal/style"
)

// FieldMultiAnnotatedShortText renders multiple annotated lines under one key.
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
	for i, text := range f.Texts {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(text)
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

// FieldMultiShortText renders multiple values under one key.
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
	for i, text := range f.Texts {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(text)
	}

	if f.ShowTotal {
		sb.WriteString("\n")
		sb.WriteString(renderDim("(" + strconv.Itoa(len(f.Texts)) + " total)"))
	}

	return renderKeyStyled(f.Title), sb.String()
}

// FieldCheckBox renders a boolean value as a check mark or cross.
// Custom TrueText and FalseText override the defaults.
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
		trueText = style.Success("\u2713")
	}
	falseText := f.FalseText
	if falseText == "" {
		falseText = style.Failure("\u2717")
	}

	if f.Boolean {
		return renderKeyStyled(f.Title), trueText
	}
	return renderKeyStyled(f.Title), falseText
}
