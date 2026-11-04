package ansi

import (
	"fmt"
	"io"
)

// A CodeBlockElement is used to render code blocks.
type CodeBlockElement struct {
	Code     string
	Language string
}

// Render renders a CodeBlockElement with the subset highlighter from
// highlight.go. Styles without a chroma section keep the plain-text fallback.
func (e *CodeBlockElement) Render(w io.Writer, ctx RenderContext) error {
	bs := ctx.blockStack

	var indentation uint
	var margin uint
	rules := ctx.options.Styles.CodeBlock
	if rules.Indent != nil {
		indentation = *rules.Indent
	}
	if rules.Margin != nil {
		margin = *rules.Margin
	}

	iw := NewIndentWriter(w, int(indentation+margin), func(_ io.Writer) {
		_, _ = renderText(w, bs.Current().Style.StylePrimitive, " ")
	})
	defer iw.Close() //nolint:errcheck

	if rules.Chroma != nil {
		_, _ = renderText(iw, bs.Current().Style.StylePrimitive, rules.BlockPrefix)
		if err := renderHighlighted(iw, e.Code, e.Language, *rules.Chroma); err != nil {
			return fmt.Errorf("glamour: error highlighting code: %w", err)
		}
		_, _ = renderText(iw, bs.Current().Style.StylePrimitive, rules.BlockSuffix)
		return nil
	}

	el := &BaseElement{
		Token: e.Code,
		Style: rules.StylePrimitive,
	}

	return el.Render(iw, ctx)
}
