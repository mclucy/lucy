package main

// plane is a summed-area table over a scalar image. It gives exact box
// filter integrals over rectangles with fractional corners, which treats
// pixels as constant squares. Areas outside the image add nothing.
type plane struct {
	w, h int
	sum  []float64
}

// newPlane builds the table from row-major values of a w×h image.
func newPlane(w, h int, values []float64) plane {
	p := plane{w: w, h: h, sum: make([]float64, (w+1)*(h+1))}
	stride := w + 1
	for y := range h {
		row := 0.0
		for x := range w {
			row += values[y*w+x]
			p.sum[(y+1)*stride+x+1] = p.sum[y*stride+x+1] + row
		}
	}
	return p
}

// cumulative is the integral over [0,x)×[0,y). It is bilinear inside each
// pixel, so interpolating the table corners is exact.
func (p plane) cumulative(x, y float64) float64 {
	x = min(max(x, 0), float64(p.w))
	y = min(max(y, 0), float64(p.h))
	x0, y0 := int(x), int(y)
	x1, y1 := min(x0+1, p.w), min(y0+1, p.h)
	fx, fy := x-float64(x0), y-float64(y0)
	stride := p.w + 1
	s00 := p.sum[y0*stride+x0]
	s10 := p.sum[y0*stride+x1]
	s01 := p.sum[y1*stride+x0]
	s11 := p.sum[y1*stride+x1]
	top := s00 + (s10-s00)*fx
	bottom := s01 + (s11-s01)*fx
	return top + (bottom-top)*fy
}

// mean is the average value over [x0,x1)×[y0,y1), counting the part outside
// the image as zero.
func (p plane) mean(x0, y0, x1, y1 float64) float64 {
	area := (x1 - x0) * (y1 - y0)
	if area <= 0 {
		return 0
	}
	total := p.cumulative(x1, y1) - p.cumulative(x0, y1) -
		p.cumulative(x1, y0) + p.cumulative(x0, y0)
	return total / area
}
