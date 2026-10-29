package main

import (
	"fmt"
	"image/color"
	"math"
	"strconv"
	"strings"
)

// oklab is a color in the OKLab perceptual space. Euclidean distance in it
// approximates perceived color difference.
type oklab struct {
	l, a, b float64
}

func srgbToLinear(v uint8) float64 {
	c := float64(v) / 255
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

func toOKLab(c color.NRGBA) oklab {
	r, g, b := srgbToLinear(c.R), srgbToLinear(c.G), srgbToLinear(c.B)
	l := math.Cbrt(0.4122214708*r + 0.5363325363*g + 0.0514459929*b)
	m := math.Cbrt(0.2119034982*r + 0.6806995451*g + 0.1073969566*b)
	s := math.Cbrt(0.0883024619*r + 0.2817188376*g + 0.6299787005*b)
	return oklab{
		l: 0.2104542553*l + 0.7936177850*m - 0.0040720468*s,
		a: 1.9779984951*l - 2.4285922050*m + 0.4505937099*s,
		b: 0.0259040371*l + 0.7827717662*m - 0.8086757660*s,
	}
}

func (o oklab) distance2(p oklab) float64 {
	dl, da, db := o.l-p.l, o.a-p.a, o.b-p.b
	return dl*dl + da*da + db*db
}

func (o oklab) chroma() float64 {
	return math.Hypot(o.a, o.b)
}

// parseHexColor accepts "#rrggbb" and "rrggbb".
func parseHexColor(s string) (color.NRGBA, error) {
	hex := strings.TrimPrefix(s, "#")
	if len(hex) != 6 {
		return color.NRGBA{}, fmt.Errorf("color %q is not #rrggbb", s)
	}
	v, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return color.NRGBA{}, fmt.Errorf("color %q is not #rrggbb", s)
	}
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 255}, nil
}

func linearToSRGB(v float64) uint8 {
	v = min(max(v, 0), 1)
	if v <= 0.0031308 {
		v *= 12.92
	} else {
		v = 1.055*math.Pow(v, 1/2.4) - 0.055
	}
	return uint8(math.Round(v * 255))
}

// toSRGB clips colors outside the sRGB gamut per channel.
func (o oklab) toSRGB() color.NRGBA {
	l := o.l + 0.3963377774*o.a + 0.2158037573*o.b
	m := o.l - 0.1055613458*o.a - 0.0638541728*o.b
	s := o.l - 0.0894841775*o.a - 1.2914855480*o.b
	l, m, s = l*l*l, m*m*m, s*s*s
	return color.NRGBA{
		R: linearToSRGB(4.0767416621*l - 3.3077115913*m + 0.2309699292*s),
		G: linearToSRGB(-1.2684380046*l + 2.6097574011*m - 0.3413193965*s),
		B: linearToSRGB(-0.0041960863*l - 0.7034186147*m + 1.7076147010*s),
		A: 255,
	}
}

// Terminal themes can be dark or light, so logo colors must stay readable
// on both. Gray colors take the terminal foreground, which every theme makes
// readable. Other colors keep their hue with lightness held in a middle band.
const (
	grayChroma       = 0.02
	terminalLightMin = 0.5
	terminalLightMax = 0.8
)

// terminalColor returns the foreground to draw a palette color with, and
// false when the terminal default foreground fits better.
func terminalColor(c color.NRGBA) (color.NRGBA, bool) {
	o := toOKLab(c)
	if o.chroma() < grayChroma {
		return color.NRGBA{}, false
	}
	o.l = min(max(o.l, terminalLightMin), terminalLightMax)
	return o.toSRGB(), true
}

func hexColor(c color.NRGBA) string {
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
}
