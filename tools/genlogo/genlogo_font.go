package main

import (
	"fmt"
	"os"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// loadCellFont parses the monospace font that glyph shapes are measured with
// and previews are drawn with.
func loadCellFont(path string) (*opentype.Font, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	f, err := opentype.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("parse font %s: %w", path, err)
	}
	return f, nil
}

// cellFace returns a face of the monospace font f whose glyph advance is
// cellWidth pixels.
func cellFace(f *opentype.Font, cellWidth int) (font.Face, error) {
	const probeSize = 1000
	probe, err := opentype.NewFace(f, &opentype.FaceOptions{Size: probeSize, DPI: 72})
	if err != nil {
		return nil, err
	}
	advance, ok := probe.GlyphAdvance('M')
	if !ok || advance <= 0 {
		return nil, fmt.Errorf("font has no advance for 'M'")
	}
	em := float64(advance) / 64 / probeSize
	return opentype.NewFace(f, &opentype.FaceOptions{
		Size:    float64(cellWidth) / em,
		DPI:     72,
		Hinting: font.HintingNone,
	})
}

// cellBaseline centers ascent and descent vertically in a cell, so that
// descenders and tall glyphs stay inside it.
func cellBaseline(face font.Face, cellHeight int) fixed.Int26_6 {
	m := face.Metrics()
	leading := fixed.I(cellHeight) - m.Ascent - m.Descent
	return leading/2 + m.Ascent
}
