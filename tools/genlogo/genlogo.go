// Command genlogo generates the status logos from the upstream images that
// the logo manifest pins. Every variant gets a plain and a colored text file
// per logo; both hold the same glyphs.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"image"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// defaultGlyphStyle draws filled shapes. Its letters give round corners and
// stems; letters that read as text instead of shape are left out.
const defaultGlyphStyle = "solid"

// glyphStyles maps a manifest glyph style to the glyphs that cells on shape
// edges choose from. The line style suits outline art, where letters along
// thin strokes read as noise.
var glyphStyles = map[string]string{
	"solid": ` .,:;_-~=+*^"'` + "`" + `/\|()<>[]{}!?ilj7LTYyvxoadbpqg#@`,
	"line":  ` .,:;_-~^"'` + "`" + `/\|()<>[]{}!`,
}

// fillGlyphs fill uniform regions, picked by ink density.
const fillGlyphs = ".:-=+*#%@"

func main() {
	defaultCache, _ := os.UserCacheDir()
	manifestPath := flag.String("manifest", "terminal/assets/logos.yaml", "logo manifest")
	outDir := flag.String("out", "terminal/assets", "directory that receives <variant>_plain/ and <variant>_colored/")
	cacheDir := flag.String("cache", filepath.Join(defaultCache, "lucy-genlogo"), "download cache directory")
	only := flag.String("only", "", "comma-separated logo ids to generate")
	previewDir := flag.String("preview", "", "also write a PNG preview of each logo into this directory")
	check := flag.Bool("check", false, "report generated files that differ from the files on disk, and write nothing")
	flag.Parse()

	m, err := loadManifest(*manifestPath)
	if err != nil {
		fatal("%v", err)
	}
	selected := map[string]bool{}
	if *only != "" {
		for id := range strings.SplitSeq(*only, ",") {
			selected[strings.TrimSpace(id)] = true
		}
	}
	gen, err := newGenerator(m, *cacheDir, *previewDir != "" && !*check)
	if err != nil {
		fatal("%v", err)
	}

	var files []outputFile
	unknown := maps.Clone(selected)
	for _, logo := range m.Logos {
		if len(selected) > 0 && !selected[logo.ID] {
			continue
		}
		delete(unknown, logo.ID)
		result, err := gen.generate(logo)
		if err != nil {
			fatal("%s: %v", logo.ID, err)
		}
		files = append(files, result.files...)
		if result.preview != nil && !*check {
			if err := writePNG(filepath.Join(*previewDir, logo.ID+".png"), result.preview); err != nil {
				fatal("%s: %v", logo.ID, err)
			}
		}
		fmt.Printf("[OK] %s palette%s\n", logo.ID, result.palette)
	}
	for id := range unknown {
		fatal("unknown logo id %q", id)
	}

	stale := []string{}
	if *only == "" {
		stale, err = staleFiles(*outDir, m)
		if err != nil {
			fatal("%v", err)
		}
	}
	if *check {
		if err := checkFiles(*outDir, files, stale); err != nil {
			fatal("%v", err)
		}
		return
	}
	if err := writeFiles(*outDir, files, stale); err != nil {
		fatal("%v", err)
	}
}

// outputFile is one generated text file, relative to the output directory.
type outputFile struct {
	path    string
	content string
}

type generator struct {
	variants []variantSpec
	cacheDir string
	glyphs   map[string]glyphSet
	options  renderOptions
	preview  *previewer
}

func newGenerator(m manifest, cacheDir string, withPreview bool) (generator, error) {
	fontPath, err := fetchSource(m.Font, cacheDir)
	if err != nil {
		return generator{}, fmt.Errorf("font: %w", err)
	}
	f, err := loadCellFont(fontPath)
	if err != nil {
		return generator{}, err
	}
	face, err := cellFace(f, glyphCellWidth)
	if err != nil {
		return generator{}, err
	}
	gen := generator{
		variants: m.Variants,
		cacheDir: cacheDir,
		glyphs:   map[string]glyphSet{},
		options: renderOptions{
			uniformity: 0.2,
			fill:       newFillRamp(face, fillGlyphs),
		},
	}
	windows := sampleWindows(3, 6, 0.5)
	for style, glyphs := range glyphStyles {
		gen.glyphs[style] = newGlyphSet(face, glyphs, windows)
	}
	if withPreview {
		p, err := newPreviewer(f)
		if err != nil {
			return generator{}, err
		}
		gen.preview = &p
	}
	return gen, nil
}

// logoResult is the output of one logo. preview is nil unless the generator
// was built with previews. palette lists the palette colors with their
// tones, to help write a fixed palette into the manifest.
type logoResult struct {
	files   []outputFile
	preview *image.NRGBA
	palette string
}

// generate renders every variant of one logo.
func (gen generator) generate(logo logoSpec) (logoResult, error) {
	path, err := fetchSource(logo.Source, gen.cacheDir)
	if err != nil {
		return logoResult{}, err
	}
	img, err := decodeImage(path, svgRasterSize)
	if err != nil {
		return logoResult{}, err
	}
	img, err = prepare(img, logo)
	if err != nil {
		return logoResult{}, err
	}
	pal, tones := logo.paletteAndTones(img)
	if logo.Despeckle > 0 {
		despeckle(img, pal, tones, logo.Despeckle)
	}
	planes := newInkPlanes(img, pal, tones)
	if planes.bounds.Empty() {
		return logoResult{}, errors.New("the source has no ink after preprocessing")
	}
	scale := 1
	if logo.Pixel {
		scale = detectPixelScale(img, planes.bounds)
		if scale == 1 {
			return logoResult{}, errors.New("pixel is set, but the source is not upscaled pixel art")
		}
	}
	style := logo.Glyphs
	if style == "" {
		style = defaultGlyphStyle
	}

	result := logoResult{}
	for i, c := range pal.colors {
		result.palette += fmt.Sprintf(" %s:%.2f", hexColor(c), tones[i])
	}
	var rows []*image.NRGBA
	for _, v := range gen.variants {
		g, ok := grid{}, false
		if scale > 1 {
			g, ok = fitPixelGrid(planes.bounds, scale, v.Cols, v.Rows)
		}
		if !ok {
			g = fitGrid(planes.bounds, v.Cols, v.Rows)
		}
		a := renderArt(planes, pal, g, gen.glyphs[style], gen.options)
		result.files = append(result.files,
			outputFile{path: filepath.Join(v.Name+"_plain", logo.ID+".txt"), content: plainText(a)},
			outputFile{path: filepath.Join(v.Name+"_colored", logo.ID+".txt"), content: ansiText(a)},
		)
		if gen.preview != nil {
			rows = append(rows, gen.preview.row(
				[]*image.NRGBA{
					gen.preview.render(a, previewDark, false),
					gen.preview.render(a, previewDark, true),
					gen.preview.render(a, previewLight, true),
				},
				[]string{fmt.Sprintf("%s %s %dx%d", logo.ID, v.Name, g.cols, g.rows)},
			))
		}
	}
	if gen.preview != nil {
		result.preview = stack(rows)
	}
	return result, nil
}

// staleFiles lists text files in the variant directories that no logo in
// the manifest generates.
func staleFiles(outDir string, m manifest) ([]string, error) {
	ids := map[string]bool{}
	for _, logo := range m.Logos {
		ids[logo.ID+".txt"] = true
	}
	var stale []string
	for _, v := range m.Variants {
		for _, dir := range []string{v.Name + "_plain", v.Name + "_colored"} {
			entries, err := os.ReadDir(filepath.Join(outDir, dir))
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
			for _, e := range entries {
				if !e.IsDir() && filepath.Ext(e.Name()) == ".txt" && !ids[e.Name()] {
					stale = append(stale, filepath.Join(dir, e.Name()))
				}
			}
		}
	}
	slices.Sort(stale)
	return stale, nil
}

func checkFiles(outDir string, files []outputFile, stale []string) error {
	var problems []string
	for _, f := range files {
		current, err := os.ReadFile(filepath.Join(outDir, f.path))
		if err != nil || !bytes.Equal(current, []byte(f.content)) {
			problems = append(problems, "outdated: "+f.path)
		}
	}
	for _, s := range stale {
		problems = append(problems, "stale: "+s)
	}
	if len(problems) > 0 {
		return fmt.Errorf("generated logos are out of date; run task logos:gen\n  %s",
			strings.Join(problems, "\n  "))
	}
	return nil
}

func writeFiles(outDir string, files []outputFile, stale []string) error {
	for _, f := range files {
		path := filepath.Join(outDir, f.path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(f.content), 0o644); err != nil {
			return err
		}
	}
	for _, s := range stale {
		if err := os.Remove(filepath.Join(outDir, s)); err != nil {
			return err
		}
		fmt.Printf("[DEL] %s\n", s)
	}
	return nil
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "genlogo: "+format+"\n", args...)
	os.Exit(1)
}
