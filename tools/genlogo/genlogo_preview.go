package main

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const (
	previewCellWidth  = 10
	previewCellHeight = previewCellWidth * 2
	previewGap        = 20
)

// previewTheme is the terminal background and default foreground a preview
// simulates.
type previewTheme struct {
	background color.NRGBA
	foreground color.NRGBA
}

var (
	previewDark = previewTheme{
		background: color.NRGBA{R: 0x1e, G: 0x1e, B: 0x1e, A: 0xff},
		foreground: color.NRGBA{R: 0xd4, G: 0xd4, B: 0xd4, A: 0xff},
	}
	previewLight = previewTheme{
		background: color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff},
		foreground: color.NRGBA{R: 0x24, G: 0x29, B: 0x2f, A: 0xff},
	}
	previewSheetBackground = color.NRGBA{R: 0x80, G: 0x80, B: 0x80, A: 0xff}
	previewCaptionColor    = color.NRGBA{R: 0x10, G: 0x10, B: 0x10, A: 0xff}
)

// previewer draws art as a terminal with the cell font would show it.
type previewer struct {
	face     font.Face
	baseline fixed.Int26_6
}

func newPreviewer(f *opentype.Font) (previewer, error) {
	face, err := cellFace(f, previewCellWidth)
	if err != nil {
		return previewer{}, err
	}
	return previewer{face: face, baseline: cellBaseline(face, previewCellHeight)}, nil
}

// render draws the art with a one-cell margin. When colored is false, every
// glyph uses the theme foreground.
func (p previewer) render(a art, theme previewTheme, colored bool) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(
		0, 0,
		(a.cols+2)*previewCellWidth,
		(a.rows+2)*previewCellHeight,
	))
	draw.Draw(img, img.Bounds(), image.NewUniform(theme.background), image.Point{}, draw.Src)
	for row := range a.rows {
		for col := range a.cols {
			cell := a.at(col, row)
			if cell.glyph == ' ' {
				continue
			}
			ink := theme.foreground
			if colored && cell.tinted {
				ink = cell.color
			}
			d := font.Drawer{
				Dst:  img,
				Src:  image.NewUniform(ink),
				Face: p.face,
				Dot: fixed.Point26_6{
					X: fixed.I((col + 1) * previewCellWidth),
					Y: fixed.I((row+1)*previewCellHeight) + p.baseline,
				},
			}
			d.DrawString(string(cell.glyph))
		}
	}
	return img
}

// row places images left to right, top-aligned, each under its caption.
func (p previewer) row(images []*image.NRGBA, captions []string) *image.NRGBA {
	width := previewGap * max(len(images)-1, 0)
	height := 0
	for _, img := range images {
		width += img.Bounds().Dx()
		height = max(height, img.Bounds().Dy())
	}
	out := image.NewNRGBA(image.Rect(0, 0, width, previewCellHeight+height))
	draw.Draw(out, out.Bounds(), image.NewUniform(previewSheetBackground), image.Point{}, draw.Src)
	x := 0
	for i, img := range images {
		if i < len(captions) && captions[i] != "" {
			d := font.Drawer{
				Dst:  out,
				Src:  image.NewUniform(previewCaptionColor),
				Face: p.face,
				Dot:  fixed.Point26_6{X: fixed.I(x), Y: p.baseline},
			}
			d.DrawString(captions[i])
		}
		bounds := img.Bounds().Add(image.Pt(x, previewCellHeight))
		draw.Draw(out, bounds, img, image.Point{}, draw.Src)
		x += img.Bounds().Dx() + previewGap
	}
	return out
}

// stack places images top to bottom with a gap.
func stack(images []*image.NRGBA) *image.NRGBA {
	width, height := 0, previewGap*max(len(images)-1, 0)
	for _, img := range images {
		width = max(width, img.Bounds().Dx())
		height += img.Bounds().Dy()
	}
	out := image.NewNRGBA(image.Rect(0, 0, width, height))
	draw.Draw(out, out.Bounds(), image.NewUniform(previewSheetBackground), image.Point{}, draw.Src)
	y := 0
	for _, img := range images {
		draw.Draw(out, img.Bounds().Add(image.Pt(0, y)), img, image.Point{}, draw.Src)
		y += img.Bounds().Dy() + previewGap
	}
	return out
}

// writePNG encodes img to path, creating parent directories.
func writePNG(path string, img image.Image) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(file, img); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
