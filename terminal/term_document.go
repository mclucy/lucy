// Package terminal provides key-value terminal output.
//
// Data contains the fields to render. Flush writes the composed output.
// Fields use Lip Gloss styles and fixed-width key columns.
package terminal

// Data is a collection of fields to render together.
type Data struct {
	Fields   []Field
	LogoMode StatusLogoMode
}

// Field is a renderable output element.
type Field interface {
	Render() string
	RenderRow() (key, value string)
	KeyLength() int
}

// WidthAware is implemented by fields that need a value-column width before
// they render multi-line or grid content.
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
