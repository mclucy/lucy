package main

import (
	"image"
	"math"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// glyphCellWidth is the raster width of one glyph cell. Cells are twice as
// tall as wide, which is the usual terminal cell shape.
const (
	glyphCellWidth  = 24
	glyphCellHeight = 2 * glyphCellWidth
)

// sampleWindow is a rectangle in unit cell coordinates: (0,0) is the top left
// corner of the cell and (1,1) the bottom right.
type sampleWindow struct {
	x0, y0, x1, y1 float64
}

// sampleWindows tiles the cell with cols×rows windows. Each window grows by
// overlap times its size on every side and is clipped to the cell. Overlap
// makes a feature respond to ink near its window, not only inside it.
func sampleWindows(cols, rows int, overlap float64) []sampleWindow {
	windows := make([]sampleWindow, 0, cols*rows)
	w, h := 1/float64(cols), 1/float64(rows)
	for row := range rows {
		for col := range cols {
			x, y := float64(col)*w, float64(row)*h
			windows = append(windows, sampleWindow{
				x0: max(0, x-overlap*w),
				y0: max(0, y-overlap*h),
				x1: min(1, x+w+overlap*w),
				y1: min(1, y+h+overlap*h),
			})
		}
	}
	return windows
}

// glyphSet holds the shape vector of each candidate glyph. Each vector
// component is the glyph's ink coverage in one sample window, divided by the
// largest coverage any glyph has in that window. A fully inked image region
// thus maps to the densest available glyph shape.
type glyphSet struct {
	glyphs  []byte
	windows []sampleWindow
	vectors [][]float64
}

func newGlyphSet(face font.Face, glyphs string, windows []sampleWindow) glyphSet {
	baseline := cellBaseline(face, glyphCellHeight)
	set := glyphSet{
		glyphs:  []byte(glyphs),
		windows: windows,
		vectors: make([][]float64, len(glyphs)),
	}
	scale := make([]float64, len(windows))
	for i, g := range set.glyphs {
		coverage := rasterizeGlyph(face, baseline, g)
		v := make([]float64, len(windows))
		for k, w := range windows {
			v[k] = coverage.mean(
				w.x0*glyphCellWidth, w.y0*glyphCellHeight,
				w.x1*glyphCellWidth, w.y1*glyphCellHeight,
			)
			scale[k] = max(scale[k], v[k])
		}
		set.vectors[i] = v
	}
	for _, v := range set.vectors {
		for k := range v {
			if scale[k] > 0 {
				v[k] /= scale[k]
			}
		}
	}
	return set
}

// fillRamp holds the glyphs that fill uniform regions, with their ink
// density relative to the densest of them.
type fillRamp struct {
	glyphs  []byte
	density []float64
}

func newFillRamp(face font.Face, glyphs string) fillRamp {
	baseline := cellBaseline(face, glyphCellHeight)
	ramp := fillRamp{glyphs: []byte(glyphs), density: make([]float64, len(glyphs))}
	densest := 0.0
	for i, g := range ramp.glyphs {
		coverage := rasterizeGlyph(face, baseline, g)
		ramp.density[i] = coverage.mean(0, 0, glyphCellWidth, glyphCellHeight)
		densest = max(densest, ramp.density[i])
	}
	for i := range ramp.density {
		ramp.density[i] /= densest
	}
	return ramp
}

// pick returns the glyph whose relative density is nearest to tone, or a
// space when tone is nearer to no ink.
func (r fillRamp) pick(tone float64) byte {
	best, bestDistance := byte(' '), tone
	for i, d := range r.density {
		if distance := math.Abs(d - tone); distance < bestDistance {
			best, bestDistance = r.glyphs[i], distance
		}
	}
	return best
}

func rasterizeGlyph(face font.Face, baseline fixed.Int26_6, g byte) plane {
	mask := image.NewAlpha(image.Rect(0, 0, glyphCellWidth, glyphCellHeight))
	d := font.Drawer{
		Dst:  mask,
		Src:  image.Opaque,
		Face: face,
		Dot:  fixed.Point26_6{Y: baseline},
	}
	d.DrawString(string(g))
	values := make([]float64, glyphCellWidth*glyphCellHeight)
	for i, a := range mask.Pix {
		values[i] = float64(a) / 255
	}
	return newPlane(glyphCellWidth, glyphCellHeight, values)
}

// match returns the index of the glyph whose shape vector is nearest to v.
// Ties resolve to the earlier glyph, so output is deterministic.
func (s glyphSet) match(v []float64) int {
	best, bestDistance := 0, -1.0
	for i, g := range s.vectors {
		d := 0.0
		for k, x := range v {
			diff := x - g[k]
			d += diff * diff
		}
		if bestDistance < 0 || d < bestDistance {
			best, bestDistance = i, d
		}
	}
	return best
}
