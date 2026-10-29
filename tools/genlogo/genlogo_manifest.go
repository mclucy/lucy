package main

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// manifest pins every input of the generated logos: the font that glyph
// shapes are measured with, the art sizes, and one upstream image per logo.
type manifest struct {
	Font     source        `yaml:"font"`
	Variants []variantSpec `yaml:"variants"`
	Logos    []logoSpec    `yaml:"logos"`
}

// variantSpec is one art size. Art fills the box on its limiting axis.
type variantSpec struct {
	Name string `yaml:"name"`
	Cols int    `yaml:"cols"`
	Rows int    `yaml:"rows"`
}

type logoSpec struct {
	ID     string `yaml:"id"`
	Source source `yaml:"source"`
	// Crop is x0, y0, x1, y1 in source pixels. For SVG, pixels of the
	// raster at svgRasterSize.
	Crop []int `yaml:"crop,omitempty"`
	// Blur smooths the source with this radius in source pixels before the
	// palette is taken. It turns fine texture into the shading of larger
	// faces, which reads better at terminal size.
	Blur int `yaml:"blur,omitempty"`
	// Knockout clears the background that touches the image border. The
	// value is the largest OKLab step between neighbor pixels that still
	// belongs to the background, so gradients clear too.
	Knockout float64 `yaml:"knockout,omitempty"`
	// Pixel marks upscaled pixel art. Cells then align to art pixels.
	Pixel bool `yaml:"pixel,omitempty"`
	// Flat gives every color full ink. The plain art is then a silhouette,
	// and only color tells regions apart. This suits gradients, which
	// otherwise turn into a noisy mix of fill glyphs.
	Flat bool `yaml:"flat,omitempty"`
	// Glyphs names the glyph style of shape edges: solid (the default) or
	// line.
	Glyphs string `yaml:"glyphs,omitempty"`
	// Despeckle clears ink islands smaller than this share of all ink, such
	// as lettering around a badge.
	Despeckle float64 `yaml:"despeckle,omitempty"`
	// Colors is the k-means palette size when Palette is empty.
	Colors  int           `yaml:"colors,omitempty"`
	Palette []paletteSpec `yaml:"palette,omitempty"`
}

// paletteSpec fixes one palette color. Tone is its ink density from 0 to 1;
// 0 makes the color background. Without a tone, density follows lightness.
type paletteSpec struct {
	Color string   `yaml:"color"`
	Tone  *float64 `yaml:"tone,omitempty"`
}

const defaultPaletteColors = 4

func loadManifest(path string) (manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return manifest{}, err
	}
	var m manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return manifest{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := m.validate(); err != nil {
		return manifest{}, fmt.Errorf("%s: %w", path, err)
	}
	return m, nil
}

func (m manifest) validate() error {
	if len(m.Variants) == 0 {
		return fmt.Errorf("no variants")
	}
	for _, v := range m.Variants {
		if !filepath.IsLocal(v.Name) || v.Cols <= 0 || v.Rows <= 0 {
			return fmt.Errorf("variant %q: need a plain name and positive cols and rows", v.Name)
		}
	}
	seen := map[string]bool{}
	for _, logo := range m.Logos {
		if !filepath.IsLocal(logo.ID) || filepath.Base(logo.ID) != logo.ID {
			return fmt.Errorf("logo %q: id must be a plain file name", logo.ID)
		}
		if seen[logo.ID] {
			return fmt.Errorf("logo %q: duplicate id", logo.ID)
		}
		seen[logo.ID] = true
		if logo.Crop != nil &&
			(len(logo.Crop) != 4 || logo.Crop[0] >= logo.Crop[2] || logo.Crop[1] >= logo.Crop[3]) {
			return fmt.Errorf("logo %q: crop must be [x0, y0, x1, y1] with x0 < x1 and y0 < y1", logo.ID)
		}
		if logo.Colors < 0 || logo.Knockout < 0 || logo.Blur < 0 {
			return fmt.Errorf("logo %q: colors, knockout, and blur must not be negative", logo.ID)
		}
		if logo.Despeckle < 0 || logo.Despeckle >= 1 {
			return fmt.Errorf("logo %q: despeckle must be at least 0 and below 1", logo.ID)
		}
		if _, ok := glyphStyles[logo.Glyphs]; logo.Glyphs != "" && !ok {
			return fmt.Errorf("logo %q: unknown glyph style %q", logo.ID, logo.Glyphs)
		}
		for _, p := range logo.Palette {
			if _, err := parseHexColor(p.Color); err != nil {
				return fmt.Errorf("logo %q: %w", logo.ID, err)
			}
			if p.Tone != nil && (*p.Tone < 0 || *p.Tone > 1) {
				return fmt.Errorf("logo %q: tone of %s must be within 0 and 1", logo.ID, p.Color)
			}
		}
	}
	return nil
}

// paletteAndTones returns the fixed palette of the logo when it has one, and
// otherwise the k-means palette of img, with the ink tone of each color.
// Colors without a fixed tone spread over the tone range by lightness.
func (logo logoSpec) paletteAndTones(img *image.NRGBA) (palette, []float64) {
	if len(logo.Palette) == 0 {
		k := logo.Colors
		if k == 0 {
			k = defaultPaletteColors
		}
		pal := extractPalette(img, k)
		return pal, logo.defaultTones(pal.lab)
	}
	colors := make([]color.NRGBA, len(logo.Palette))
	fixed := map[color.NRGBA]float64{}
	for i, p := range logo.Palette {
		colors[i], _ = parseHexColor(p.Color)
		if p.Tone != nil {
			fixed[colors[i]] = *p.Tone
		}
	}
	pal := newPalette(colors)
	var free []int
	var freeLab []oklab
	tones := make([]float64, len(pal.colors))
	for i, c := range pal.colors {
		if tone, ok := fixed[c]; ok {
			tones[i] = tone
			continue
		}
		free = append(free, i)
		freeLab = append(freeLab, pal.lab[i])
	}
	for j, tone := range logo.defaultTones(freeLab) {
		tones[free[j]] = tone
	}
	return pal, tones
}

func (logo logoSpec) defaultTones(colors []oklab) []float64 {
	if !logo.Flat {
		return lightnessTones(colors, toneFloor)
	}
	tones := make([]float64, len(colors))
	for i := range tones {
		tones[i] = 1
	}
	return tones
}
