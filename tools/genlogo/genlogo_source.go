package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
	_ "golang.org/x/image/webp"
)

const (
	sourceAttempts = 3
	sourceTimeout  = 60 * time.Second
	sourceAgent    = "lucy-genlogo"
)

// source pins one upstream image.
type source struct {
	URL    string `yaml:"url"`
	SHA256 string `yaml:"sha256"`
}

// digestError reports a download whose bytes do not match the pinned digest.
type digestError struct {
	url  string
	got  string
	want string
}

func (e digestError) Error() string {
	return fmt.Sprintf("sha256 mismatch for %s: got %s want %s", e.url, e.got, e.want)
}

// fetchSource returns the path of the cached, digest-verified source file and
// downloads it on a cache miss.
func fetchSource(src source, cacheDir string) (string, error) {
	digest, err := normalizeDigest(src.SHA256)
	if err != nil {
		return "", err
	}
	path := filepath.Join(cacheDir, digest[:2], digest)
	hit, err := hasDigest(path, digest)
	if err != nil {
		return "", err
	}
	if hit {
		return path, nil
	}
	// A file with another digest cannot become valid, so drop it.
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("drop stale cache %s: %w", path, err)
	}
	if err := downloadSource(src.URL, path, digest); err != nil {
		return "", err
	}
	return path, nil
}

// normalizeDigest accepts hex in either case and returns lowercase hex.
func normalizeDigest(raw string) (string, error) {
	digest := strings.ToLower(strings.TrimSpace(raw))
	if len(digest) != sha256.Size*2 {
		return "", fmt.Errorf("sha256 must be 64 hex characters, got %d", len(digest))
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return "", fmt.Errorf("sha256 %q is not hex: %w", raw, err)
	}
	return digest, nil
}

// hasDigest reports whether path exists with the wanted digest.
func hasDigest(path, want string) (bool, error) {
	got, err := fileDigest(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return got == want, nil
}

func fileDigest(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("hash %s: %w", path, err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// downloadSource retries network failures but not a digest mismatch, which
// retrying cannot fix.
func downloadSource(url, path, want string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create cache dir %s: %w", dir, err)
	}
	client := &http.Client{Timeout: sourceTimeout}
	var lastErr error
	for range sourceAttempts {
		lastErr = downloadOnce(client, url, path, want)
		if lastErr == nil {
			return nil
		}
		var bad digestError
		if errors.As(lastErr, &bad) {
			break
		}
	}
	return lastErr
}

func downloadOnce(client *http.Client, url, path, want string) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build request for %s: %w", url, err)
	}
	req.Header.Set("User-Agent", sourceAgent)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("download %s: %s", url, resp.Status)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".download-*")
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", path, err)
	}
	tmpName := tmp.Name()
	done := false
	defer func() {
		if done {
			return
		}
		tmp.Close()
		os.Remove(tmpName)
	}()
	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, hash), resp.Body); err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write %s: %w", tmpName, err)
	}
	got := hex.EncodeToString(hash.Sum(nil))
	if got != want {
		return digestError{url: url, got: got, want: want}
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("store %s: %w", path, err)
	}
	done = true
	return nil
}

// decodeImage decodes PNG, JPEG, GIF, WebP, or SVG into straight-alpha NRGBA.
// It sniffs the content, not the file name. It rasterizes SVG so that the
// longer side is svgSize pixels.
func decodeImage(path string, svgSize int) (*image.NRGBA, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if isSVG(data) {
		img, err := rasterizeSVG(data, svgSize)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		return img, nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return straightAlpha(img), nil
}

// isSVG reports whether the content, after any BOM and XML preamble, has an
// svg root. Upstream URLs do not always carry an .svg suffix.
func isSVG(data []byte) bool {
	rest := bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	for {
		rest = bytes.TrimLeft(rest, " \t\r\n")
		var cut bool
		switch {
		case bytes.HasPrefix(rest, []byte("<?")):
			rest, cut = skipPast(rest, []byte("?>"))
		case bytes.HasPrefix(rest, []byte("<!--")):
			rest, cut = skipPast(rest, []byte("-->"))
		case bytes.HasPrefix(rest, []byte("<!DOCTYPE")),
			bytes.HasPrefix(rest, []byte("<!doctype")):
			rest, cut = skipPast(rest, []byte(">"))
		default:
			return bytes.Contains(rest, []byte("<svg"))
		}
		if !cut {
			return false
		}
	}
}

// skipPast returns the content after the first delim, and false when absent.
func skipPast(data, delim []byte) ([]byte, bool) {
	idx := bytes.Index(data, delim)
	if idx < 0 {
		return nil, false
	}
	return data[idx+len(delim):], true
}

// rasterizeSVG draws data with a transparent background. oksvg and rasterx
// panic on some valid-looking input, so a panic becomes an error.
func rasterizeSVG(data []byte, size int) (img *image.NRGBA, err error) {
	if size < 1 {
		return nil, fmt.Errorf("svg size %d is below 1 pixel", size)
	}
	defer func() {
		if reason := recover(); reason != nil {
			img = nil
			err = fmt.Errorf("rasterize svg: %v", reason)
		}
	}()
	icon, err := oksvg.ReadIconStream(bytes.NewReader(data), oksvg.WarnErrorMode)
	if err != nil {
		return nil, fmt.Errorf("parse svg: %w", err)
	}
	width, height := svgExtent(data, icon.ViewBox.W, icon.ViewBox.H)
	scale := float64(size) / max(width, height)
	cols := max(1, int(math.Round(width*scale)))
	rows := max(1, int(math.Round(height*scale)))
	// Scale before translating: oksvg's SetTarget translates by the unscaled
	// viewBox origin, which shifts icons with a non-zero origin.
	icon.Transform = rasterx.Identity.Scale(scale, scale).Translate(
		-icon.ViewBox.X,
		-icon.ViewBox.Y,
	)
	// oksvg transforms path points before stroking, so line widths and dashes
	// stay in user units and would render too thin on a scaled raster.
	for i := range icon.SVGPaths {
		path := &icon.SVGPaths[i]
		path.LineWidth *= scale
		for j := range path.Dash {
			path.Dash[j] *= scale
		}
	}
	canvas := image.NewRGBA(image.Rect(0, 0, cols, rows))
	scanner := rasterx.NewScannerGV(cols, rows, canvas, canvas.Bounds())
	icon.Draw(rasterx.NewDasher(cols, rows, scanner), 1)
	return straightAlpha(canvas), nil
}

// svgExtent falls back to the root width and height when the viewBox has no
// positive extent. Without either, the drawing is assumed square.
func svgExtent(data []byte, width, height float64) (float64, float64) {
	if width > 0 && height > 0 {
		return width, height
	}
	attrW, attrH := rootLengths(data)
	if width <= 0 {
		width = attrW
	}
	if height <= 0 {
		height = attrH
	}
	if width <= 0 || height <= 0 {
		return 1, 1
	}
	return width, height
}

func rootLengths(data []byte) (width, height float64) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	for {
		token, err := decoder.Token()
		if err != nil {
			return 0, 0
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		if start.Name.Local != "svg" {
			return 0, 0
		}
		for _, attr := range start.Attr {
			switch attr.Name.Local {
			case "width":
				width = lengthPixels(attr.Value)
			case "height":
				height = lengthPixels(attr.Value)
			}
		}
		return width, height
	}
}

// lengthPixels parses a length as user units. Only the width to height ratio
// matters, so physical units are not converted.
func lengthPixels(value string) float64 {
	value = strings.TrimSpace(value)
	for _, unit := range []string{"px", "pt", "pc", "mm", "cm", "in", "em", "ex", "%"} {
		value = strings.TrimSuffix(value, unit)
	}
	length, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return 0
	}
	return length
}

// straightAlpha converts any image to non-premultiplied NRGBA. draw.Draw
// performs the unpremultiply step when the destination is NRGBA.
func straightAlpha(src image.Image) *image.NRGBA {
	if img, ok := src.(*image.NRGBA); ok {
		return img
	}
	bounds := src.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(dst, dst.Bounds(), src, bounds.Min, draw.Src)
	return dst
}
