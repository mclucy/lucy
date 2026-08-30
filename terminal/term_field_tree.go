package terminal

import (
	"strings"

	"charm.land/lipgloss/v2/tree"
	"github.com/mclucy/lucy/terminal/style"
)

type FieldTree struct {
	Title      string
	Text       string
	Annotation string
	Children   []TreeNode

	availableWidth int
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

// RenderRow returns the parent row without its children.
func (f *FieldTree) RenderRow() (key, value string) {
	var sb strings.Builder
	sb.WriteString(f.Text)
	if f.Annotation != "" {
		sb.WriteString(renderAnnot(f.Annotation))
	}
	return renderKeyStyled(f.Title), sb.String()
}

// RenderChildren renders the child tree at column 0.
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
		label := treeChildLabel(child, childKeyWidth, f.availableWidth)
		t.Child(label)
	}
	return t.String()
}

// HasChildren reports whether this field has child nodes.
func (f *FieldTree) HasChildren() bool {
	return len(f.Children) > 0
}

func (f *FieldTree) SetAvailableWidth(width int) {
	f.availableWidth = width
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

func treeChildValueWidth(keyWidth int, blockWidth int) int {
	if blockWidth > 0 {
		w := blockWidth - keyWidth
		if w > 0 {
			return w
		}
		return 1
	}
	w := style.TermWidth() - keyWidth
	if w > 0 {
		return w
	}
	return 1
}

func treeChildLabel(node TreeNode, keyWidth int, blockWidth int) string {
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
			widthAware.SetAvailableWidth(treeChildValueWidth(keyWidth, blockWidth))
		}
		_, value := field.RenderRow()
		value = strings.TrimSuffix(value, "\n")
		return treeChildKeyWithMultilineValue(key, value, keyWidth)
	case *FieldMultiAnnotatedShortText:
		_, value := field.RenderRow()
		value = strings.TrimSuffix(value, "\n")
		return treeChildKeyWithMultilineValue(key, value, keyWidth)
	case *FieldMultiShortText:
		if len(field.Texts) > 0 {
			return key + field.Texts[0]
		}
		return key
	default:
		return strings.TrimSuffix(node.Field.Render(), "\n")
	}
}

func treeChildKeyWithMultilineValue(key, value string, keyWidth int) string {
	if value == "" {
		return key
	}
	lines := strings.Split(value, "\n")
	if len(lines) == 1 {
		return key + lines[0]
	}
	indent := strings.Repeat(" ", keyWidth)
	var sb strings.Builder
	sb.WriteString(key)
	sb.WriteString(lines[0])
	for _, line := range lines[1:] {
		sb.WriteString("\n")
		sb.WriteString(indent)
		sb.WriteString(line)
	}
	return sb.String()
}
