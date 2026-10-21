package main

import (
	"image"
	"image/color"
	"slices"
)

// palette holds the flat colors that a logo is reduced to, sorted from dark
// to light.
type palette struct {
	colors []color.NRGBA
	lab    []oklab
}

func newPalette(colors []color.NRGBA) palette {
	p := palette{colors: slices.Clone(colors)}
	slices.SortStableFunc(p.colors, func(x, y color.NRGBA) int {
		lx, ly := toOKLab(x).l, toOKLab(y).l
		switch {
		case lx < ly:
			return -1
		case lx > ly:
			return 1
		default:
			return 0
		}
	})
	p.lab = make([]oklab, len(p.colors))
	for i, c := range p.colors {
		p.lab[i] = toOKLab(c)
	}
	return p
}

func (p palette) nearest(o oklab) int {
	best, bestDistance := 0, -1.0
	for i, lab := range p.lab {
		if d := o.distance2(lab); bestDistance < 0 || d < bestDistance {
			best, bestDistance = i, d
		}
	}
	return best
}

// colorBin accumulates the opaque pixels that share one quantized color.
type colorBin struct {
	weight  float64
	r, g, b float64
	lab     oklab
	cluster int
}

// paletteMinShare drops clusters that cover less than this share of the
// opaque area. Such clusters are usually antialiasing between two flat colors.
const paletteMinShare = 0.01

// extractPalette reduces the opaque pixels of img to at most k flat colors
// with weighted k-means in OKLab. Each output color is the most frequent
// source color of its cluster, so flat brand colors stay exact.
func extractPalette(img *image.NRGBA, k int) palette {
	bins := histogram(img)
	if len(bins) == 0 || k <= 0 {
		return palette{}
	}
	k = min(k, len(bins))
	centers := seedCenters(bins, k)
	for range 64 {
		changed := assignClusters(bins, centers)
		centers = updateCenters(bins, centers)
		if !changed {
			break
		}
	}
	centers = dropMinorCenters(bins, centers)
	assignClusters(bins, centers)

	medoids := make([]*colorBin, len(centers))
	for i := range bins {
		bin := &bins[i]
		if m := medoids[bin.cluster]; m == nil || bin.weight > m.weight {
			medoids[bin.cluster] = bin
		}
	}
	colors := make([]color.NRGBA, 0, len(medoids))
	for _, m := range medoids {
		if m == nil {
			continue
		}
		colors = append(colors, color.NRGBA{
			R: uint8(m.r/m.weight + 0.5),
			G: uint8(m.g/m.weight + 0.5),
			B: uint8(m.b/m.weight + 0.5),
			A: 255,
		})
	}
	return newPalette(colors)
}

// histogram groups pixels with alpha of at least one half by color quantized
// to 5 bits per channel. Weights are alpha, so faint edges count less.
func histogram(img *image.NRGBA) []colorBin {
	index := map[uint16]int{}
	var bins []colorBin
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := img.NRGBAAt(x, y)
			if c.A < 128 {
				continue
			}
			key := uint16(c.R>>3)<<10 | uint16(c.G>>3)<<5 | uint16(c.B>>3)
			i, ok := index[key]
			if !ok {
				i = len(bins)
				index[key] = i
				bins = append(bins, colorBin{})
			}
			w := float64(c.A) / 255
			bins[i].weight += w
			bins[i].r += w * float64(c.R)
			bins[i].g += w * float64(c.G)
			bins[i].b += w * float64(c.B)
		}
	}
	for i := range bins {
		bin := &bins[i]
		bin.lab = toOKLab(color.NRGBA{
			R: uint8(bin.r/bin.weight + 0.5),
			G: uint8(bin.g/bin.weight + 0.5),
			B: uint8(bin.b/bin.weight + 0.5),
			A: 255,
		})
	}
	// Map iteration order is random; sort so that seeding is reproducible.
	slices.SortFunc(bins, func(x, y colorBin) int {
		switch {
		case x.weight > y.weight:
			return -1
		case x.weight < y.weight:
			return 1
		}
		return cmpLab(x.lab, y.lab)
	})
	return bins
}

func cmpLab(x, y oklab) int {
	for _, d := range [3]float64{x.l - y.l, x.a - y.a, x.b - y.b} {
		if d < 0 {
			return -1
		}
		if d > 0 {
			return 1
		}
	}
	return 0
}

// seedCenters is a deterministic k-means++: it starts at the heaviest color
// and then takes the color with the largest weight times squared distance to
// the chosen centers.
func seedCenters(bins []colorBin, k int) []oklab {
	centers := []oklab{bins[0].lab}
	nearest := make([]float64, len(bins))
	for i := range bins {
		nearest[i] = bins[i].lab.distance2(centers[0])
	}
	for len(centers) < k {
		best, bestScore := -1, 0.0
		for i := range bins {
			if score := bins[i].weight * nearest[i]; score > bestScore {
				best, bestScore = i, score
			}
		}
		if best < 0 {
			break
		}
		centers = append(centers, bins[best].lab)
		for i := range bins {
			nearest[i] = min(nearest[i], bins[i].lab.distance2(bins[best].lab))
		}
	}
	return centers
}

func assignClusters(bins []colorBin, centers []oklab) bool {
	p := palette{lab: centers}
	changed := false
	for i := range bins {
		c := p.nearest(bins[i].lab)
		if c != bins[i].cluster {
			bins[i].cluster = c
			changed = true
		}
	}
	return changed
}

func updateCenters(bins []colorBin, centers []oklab) []oklab {
	sums := make([]oklab, len(centers))
	weights := make([]float64, len(centers))
	for _, bin := range bins {
		s := &sums[bin.cluster]
		s.l += bin.weight * bin.lab.l
		s.a += bin.weight * bin.lab.a
		s.b += bin.weight * bin.lab.b
		weights[bin.cluster] += bin.weight
	}
	next := make([]oklab, len(centers))
	for i, w := range weights {
		if w == 0 {
			next[i] = centers[i]
			continue
		}
		next[i] = oklab{l: sums[i].l / w, a: sums[i].a / w, b: sums[i].b / w}
	}
	return next
}

func dropMinorCenters(bins []colorBin, centers []oklab) []oklab {
	weights := make([]float64, len(centers))
	total := 0.0
	for _, bin := range bins {
		weights[bin.cluster] += bin.weight
		total += bin.weight
	}
	kept := centers[:0:0]
	for i, c := range centers {
		if weights[i] >= paletteMinShare*total {
			kept = append(kept, c)
		}
	}
	if len(kept) == 0 {
		return centers
	}
	return kept
}
