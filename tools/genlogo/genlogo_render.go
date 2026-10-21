package main

import (
	"image"
	"image/color"
	"math"
)

// cellAspect is terminal cell width over height.
const cellAspect = 0.5

// grid maps terminal cells onto source pixels.
type grid struct {
	cols, rows   int
	x0, y0       float64
	cellW, cellH float64
}

// fitGrid scales content to fill a box of maxCols×maxRows cells on its
// limiting axis, rounds the other axis to whole cells, and centers it.
func fitGrid(content image.Rectangle, maxCols, maxRows int) grid {
	w, h := float64(content.Dx()), float64(content.Dy())
	cols, rows := maxCols, maxRows
	if fitRows := h * cellAspect * float64(maxCols) / w; fitRows <= float64(maxRows) {
		rows = max(1, int(math.Round(fitRows)))
	} else {
		cols = max(1, int(math.Round(w*float64(maxRows)/(h*cellAspect))))
	}
	cellW := max(w/float64(cols), h*cellAspect/float64(rows))
	cellH := cellW / cellAspect
	return grid{
		cols:  cols,
		rows:  rows,
		x0:    float64(content.Min.X) - (float64(cols)*cellW-w)/2,
		y0:    float64(content.Min.Y) - (float64(rows)*cellH-h)/2,
		cellW: cellW,
		cellH: cellH,
	}
}

// pixelCellRatios lists how many cells one art pixel spans horizontally,
// largest first. Each keeps cells aligned to art pixel edges.
var pixelCellRatios = []float64{4, 3, 2, 1, 0.5}

// fitPixelGrid maps each art pixel of side scale source pixels to whole or
// half cells, so every cell edge lies on an art pixel edge. It reports false
// when even the smallest ratio does not fit the box.
func fitPixelGrid(content image.Rectangle, scale, maxCols, maxRows int) (grid, bool) {
	w := float64(content.Dx()) / float64(scale)
	h := float64(content.Dy()) / float64(scale)
	for _, ratio := range pixelCellRatios {
		cols := int(math.Ceil(w * ratio))
		rows := int(math.Ceil(h * ratio * cellAspect))
		if cols > maxCols || rows > maxRows {
			continue
		}
		cellW := float64(scale) / ratio
		return grid{
			cols:  cols,
			rows:  rows,
			x0:    float64(content.Min.X),
			y0:    float64(content.Min.Y),
			cellW: cellW,
			cellH: cellW / cellAspect,
		}, true
	}
	return grid{}, false
}

// detectPixelScale returns the side, in source pixels, of the square blocks
// that an upscaled pixel art image is made of, or 1 when it is not pixel art.
func detectPixelScale(img *image.NRGBA, content image.Rectangle) int {
	for scale := min(content.Dx(), content.Dy()) / 4; scale >= 2; scale-- {
		if content.Dx()%scale != 0 || content.Dy()%scale != 0 {
			continue
		}
		if uniformBlockShare(img, content, scale) >= 0.97 {
			return scale
		}
	}
	return 1
}

func uniformBlockShare(img *image.NRGBA, content image.Rectangle, scale int) float64 {
	uniform, total := 0, 0
	for by := content.Min.Y; by < content.Max.Y; by += scale {
		for bx := content.Min.X; bx < content.Max.X; bx += scale {
			total++
			if blockIsUniform(img, bx, by, scale) {
				uniform++
			}
		}
	}
	return float64(uniform) / float64(total)
}

func blockIsUniform(img *image.NRGBA, bx, by, scale int) bool {
	ref := img.NRGBAAt(bx, by)
	for y := by; y < by+scale; y++ {
		for x := bx; x < bx+scale; x++ {
			c := img.NRGBAAt(x, y)
			if ref.A == 0 && c.A == 0 {
				continue
			}
			if channelDiff(ref.R, c.R) > 8 || channelDiff(ref.G, c.G) > 8 ||
				channelDiff(ref.B, c.B) > 8 || channelDiff(ref.A, c.A) > 8 {
				return false
			}
		}
	}
	return true
}

func channelDiff(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}

// inkThreshold is the least ink that counts as content when trimming.
const inkThreshold = 0.03

// inkPlanes holds the source image reduced to its palette. tone is the ink
// density of every pixel; labels[k] is the alpha of pixels that belong to
// palette color k. bounds encloses all pixels with ink.
type inkPlanes struct {
	tone   plane
	labels []plane
	bounds image.Rectangle
}

// newInkPlanes expects img to start at the origin. Pixels of a color with
// tone 0 count as background.
func newInkPlanes(img *image.NRGBA, pal palette, tones []float64) inkPlanes {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	tone := make([]float64, w*h)
	labels := make([][]float64, len(pal.colors))
	for k := range labels {
		labels[k] = make([]float64, w*h)
	}
	bounds := image.Rectangle{}
	cache := map[color.NRGBA]int{}
	for y := range h {
		for x := range w {
			c := img.NRGBAAt(x, y)
			if c.A == 0 {
				continue
			}
			opaque := color.NRGBA{R: c.R, G: c.G, B: c.B, A: 255}
			k, ok := cache[opaque]
			if !ok {
				k = pal.nearest(toOKLab(opaque))
				cache[opaque] = k
			}
			if tones[k] == 0 {
				continue
			}
			alpha := float64(c.A) / 255
			tone[y*w+x] = alpha * tones[k]
			labels[k][y*w+x] = alpha
			if tone[y*w+x] >= inkThreshold {
				bounds = bounds.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	planes := inkPlanes{
		tone:   newPlane(w, h, tone),
		labels: make([]plane, len(labels)),
		bounds: bounds,
	}
	for k, values := range labels {
		planes.labels[k] = newPlane(w, h, values)
	}
	return planes
}

// renderOptions tunes how renderArt turns ink into glyphs.
type renderOptions struct {
	// uniformity is the largest spread of window ink inside a cell that
	// still counts as a uniform region. Such cells take a fill glyph, so
	// close tones inside a region do not turn into shape glyphs.
	uniformity float64
	fill       fillRamp
}

// renderArt fills uniform cells from the fill ramp and gives every other
// cell the glyph whose shape best matches its ink. Each glyph takes the
// palette color that lies under its ink.
func renderArt(planes inkPlanes, pal palette, g grid, set glyphSet, opts renderOptions) art {
	out := newArt(g.cols, g.rows)
	inks := make([]artCell, len(pal.colors))
	for i, c := range pal.colors {
		inks[i].color, inks[i].tinted = terminalColor(c)
	}
	v := make([]float64, len(set.windows))
	rects := make([][4]float64, len(set.windows))
	flat := make([]float64, len(set.windows))
	for k := range flat {
		flat[k] = 1
	}
	for row := range g.rows {
		for col := range g.cols {
			cx := g.x0 + float64(col)*g.cellW
			cy := g.y0 + float64(row)*g.cellH
			peak, floor, sum := 0.0, math.Inf(1), 0.0
			for k, w := range set.windows {
				rects[k] = [4]float64{
					cx + w.x0*g.cellW, cy + w.y0*g.cellH,
					cx + w.x1*g.cellW, cy + w.y1*g.cellH,
				}
				v[k] = planes.tone.mean(rects[k][0], rects[k][1], rects[k][2], rects[k][3])
				peak = max(peak, v[k])
				floor = min(floor, v[k])
				sum += v[k]
			}
			if peak == 0 {
				continue
			}
			glyph, weights := byte(0), flat
			if peak-floor <= opts.uniformity {
				glyph = opts.fill.pick(sum / float64(len(v)))
			} else {
				i := set.match(v)
				glyph, weights = set.glyphs[i], set.vectors[i]
			}
			if glyph == ' ' {
				continue
			}
			best, bestScore := 0, -1.0
			for label, p := range planes.labels {
				score := 0.0
				for k, r := range rects {
					score += weights[k] * p.mean(r[0], r[1], r[2], r[3])
				}
				if score > bestScore {
					best, bestScore = label, score
				}
			}
			cell := inks[best]
			cell.glyph = glyph
			out.set(col, row, cell)
		}
	}
	return out
}

// toneFloor is the lowest tone that lightnessTones gives. Darker colors
// still need visible ink, or they vanish on dark terminals.
const toneFloor = 0.35

// lightnessTones gives each color an ink density that rises with its OKLab
// lightness from floor to 1. A single color gets full ink.
func lightnessTones(colors []oklab, floor float64) []float64 {
	tones := make([]float64, len(colors))
	if len(tones) == 0 {
		return tones
	}
	lo, hi := colors[0].l, colors[0].l
	for _, c := range colors {
		lo, hi = min(lo, c.l), max(hi, c.l)
	}
	for i, c := range colors {
		if hi-lo < 1e-6 {
			tones[i] = 1
			continue
		}
		tones[i] = floor + (1-floor)*(c.l-lo)/(hi-lo)
	}
	return tones
}
