package main

import (
	"fmt"
	"image"
	"image/color"
	stddraw "image/draw"

	"golang.org/x/image/draw"
)

const (
	// svgRasterSize is the longer side of rasterized SVG sources.
	svgRasterSize = 1024
	// workingSize caps the longer side of raster sources other than pixel
	// art. At the largest variant it still gives each cell more than
	// ten source pixels per side.
	workingSize = 1024
)

// prepare crops, clears the background, smooths, and scales the source as
// the logo asks. The result always starts at the origin.
func prepare(img *image.NRGBA, logo logoSpec) (*image.NRGBA, error) {
	if logo.Crop != nil {
		r := image.Rect(logo.Crop[0], logo.Crop[1], logo.Crop[2], logo.Crop[3])
		if !r.In(img.Bounds()) {
			return nil, fmt.Errorf("crop %v is outside the source bounds %v", r, img.Bounds())
		}
		img = rebase(img.SubImage(r).(*image.NRGBA))
	}
	if logo.Knockout > 0 {
		knockout(img, logo.Knockout)
	}
	if logo.Blur > 0 {
		blur(img, logo.Blur)
	}
	if !logo.Pixel {
		img = downscale(img, workingSize)
	}
	return img, nil
}

func rebase(img *image.NRGBA) *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, img.Bounds().Dx(), img.Bounds().Dy()))
	stddraw.Draw(out, out.Bounds(), img, img.Bounds().Min, stddraw.Src)
	return out
}

// knockout makes transparent every pixel reachable from the image border
// through steps between neighbors of at most tolerance in OKLab.
func knockout(img *image.NRGBA, tolerance float64) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	limit := tolerance * tolerance
	cleared := make([]bool, w*h)
	lab := func(x, y int) oklab {
		c := img.NRGBAAt(x, y)
		return toOKLab(color.NRGBA{R: c.R, G: c.G, B: c.B, A: 255})
	}
	var queue []image.Point
	for x := range w {
		queue = append(queue, image.Pt(x, 0), image.Pt(x, h-1))
	}
	for y := range h {
		queue = append(queue, image.Pt(0, y), image.Pt(w-1, y))
	}
	for _, p := range queue {
		cleared[p.Y*w+p.X] = true
	}
	for len(queue) > 0 {
		p := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		from := lab(p.X, p.Y)
		transparent := img.NRGBAAt(p.X, p.Y).A == 0
		for _, d := range [4]image.Point{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			n := p.Add(d)
			if n.X < 0 || n.Y < 0 || n.X >= w || n.Y >= h || cleared[n.Y*w+n.X] {
				continue
			}
			next := img.NRGBAAt(n.X, n.Y)
			if next.A != 0 && (transparent || from.distance2(lab(n.X, n.Y)) > limit) {
				continue
			}
			cleared[n.Y*w+n.X] = true
			queue = append(queue, n)
		}
	}
	for i, c := range cleared {
		if c {
			img.Pix[i*4+3] = 0
		}
	}
}

// downscale shrinks img so that its longer side is at most size. The
// bilinear kernel widens with the scale factor, so it averages every source
// pixel instead of sampling a few.
func downscale(img *image.NRGBA, size int) *image.NRGBA {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	if max(w, h) <= size {
		return img
	}
	scale := float64(size) / float64(max(w, h))
	dst := image.NewRGBA(image.Rect(
		0, 0,
		max(1, int(float64(w)*scale+0.5)),
		max(1, int(float64(h)*scale+0.5)),
	))
	draw.BiLinear.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Src, nil)
	return straightAlpha(dst)
}

// blur smooths img with two passes of a box filter of the given radius,
// which approximates a Gaussian. It works on premultiplied color, so
// transparent pixels do not darken edges.
func blur(img *image.NRGBA, radius int) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	channels := make([][]float64, 4)
	for c := range channels {
		channels[c] = make([]float64, w*h)
	}
	for i := range w * h {
		a := float64(img.Pix[i*4+3]) / 255
		channels[3][i] = a
		for c := range 3 {
			channels[c][i] = float64(img.Pix[i*4+c]) * a
		}
	}
	scratch := make([]float64, max(w, h))
	for _, values := range channels {
		for range 2 {
			for y := range h {
				boxBlur(values[y*w:(y+1)*w], 1, radius, scratch[:w])
			}
			for x := range w {
				boxBlur(values[x:], w, radius, scratch[:h])
			}
		}
	}
	for i := range w * h {
		a := channels[3][i]
		img.Pix[i*4+3] = uint8(min(255, a*255+0.5))
		if a <= 0 {
			continue
		}
		for c := range 3 {
			img.Pix[i*4+c] = uint8(min(255, channels[c][i]/a+0.5))
		}
	}
}

// boxBlur averages len(scratch) values that lie stride apart in values over
// a window of 2·radius+1. The window shrinks at the ends.
func boxBlur(values []float64, stride, radius int, scratch []float64) {
	n := len(scratch)
	sum, count := 0.0, 0
	for i := range min(radius, n) {
		sum += values[i*stride]
		count++
	}
	for i := range n {
		if j := i + radius; j < n {
			sum += values[j*stride]
			count++
		}
		if j := i - radius - 1; j >= 0 {
			sum -= values[j*stride]
			count--
		}
		scratch[i] = sum / float64(count)
	}
	for i := range n {
		values[i*stride] = scratch[i]
	}
}

// despeckle clears ink islands smaller than minShare of all ink. Ink is a
// pixel with alpha of at least one half whose palette color has a tone; an
// island is an 8-connected group of ink. Lettering around a badge falls
// apart into such islands, while the mark stays one large island. The
// antialiased fringe of a cleared island goes with it.
func despeckle(img *image.NRGBA, pal palette, tones []float64, minShare float64) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	ink := make([]bool, w*h)
	total := 0
	cache := map[color.NRGBA]bool{}
	for y := range h {
		for x := range w {
			c := img.NRGBAAt(x, y)
			if c.A < 128 {
				continue
			}
			opaque := color.NRGBA{R: c.R, G: c.G, B: c.B, A: 255}
			isInk, ok := cache[opaque]
			if !ok {
				isInk = tones[pal.nearest(toOKLab(opaque))] > 0
				cache[opaque] = isInk
			}
			if isInk {
				ink[y*w+x] = true
				total++
			}
		}
	}
	neighbors := func(i int, visit func(n int)) {
		x, y := i%w, i/w
		for ny := max(y-1, 0); ny <= min(y+1, h-1); ny++ {
			for nx := max(x-1, 0); nx <= min(x+1, w-1); nx++ {
				visit(ny*w + nx)
			}
		}
	}
	seen := make([]bool, w*h)
	var island, stack []int
	for start := range ink {
		if !ink[start] || seen[start] {
			continue
		}
		island, stack = island[:0], append(stack[:0], start)
		seen[start] = true
		for len(stack) > 0 {
			i := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			island = append(island, i)
			neighbors(i, func(n int) {
				if ink[n] && !seen[n] {
					seen[n] = true
					stack = append(stack, n)
				}
			})
		}
		if float64(len(island)) >= minShare*float64(total) {
			continue
		}
		for _, i := range island {
			neighbors(i, func(n int) {
				if n == i || !ink[n] {
					img.Pix[n*4+3] = 0
				}
			})
		}
	}
}
