package tui

import (
	_ "embed"
	"strings"
)

// LogoVariant selects between the large and small logo variants.
type LogoVariant int

const (
	// LogoLarge selects the full-size ASCII art logo.
	LogoLarge LogoVariant = 0
	// LogoSmall selects the compact ASCII art logo.
	LogoSmall LogoVariant = 1
)

//go:embed assets/status_logo_large.txt
var logoLargeRaw string

//go:embed assets/status_logo_small.txt
var logoSmallRaw string

// FieldLogo is a Field that holds the ASCII logo for the status view.
// It satisfies the Field interface so it can be placed in Data.Fields,
// but its primary API is the Lines / Width / Height helpers which the
// layout compositor uses to build the neofetch-style side-by-side view.
type FieldLogo struct{}

// Render returns the large logo as a plain string. This is a fallback for
// callers that are not layout-aware and simply iterate over Fields.
func (f *FieldLogo) Render() string {
	return strings.Join(normalizeLines(logoLargeRaw), "\n")
}

// KeyLength returns 0 because the logo is not a key-value field.
func (f *FieldLogo) KeyLength() int {
	return 0
}

// Lines returns the normalized lines of the requested logo variant.
// Each line is padded with trailing spaces so that all lines share the
// same width, making grid-based composition straightforward.
func (f *FieldLogo) Lines(variant LogoVariant) []string {
	return normalizeLines(rawForVariant(variant))
}

// Width returns the uniform width (in runes) of every line for the given
// logo variant.
func (f *FieldLogo) Width(variant LogoVariant) int {
	lines := normalizeLines(rawForVariant(variant))
	if len(lines) == 0 {
		return 0
	}
	return len([]rune(lines[0]))
}

// Height returns the number of lines for the given logo variant.
func (f *FieldLogo) Height(variant LogoVariant) int {
	return len(normalizeLines(rawForVariant(variant)))
}

// rawForVariant returns the raw embedded string for the given variant.
func rawForVariant(variant LogoVariant) string {
	switch variant {
	case LogoSmall:
		return logoSmallRaw
	default:
		return logoLargeRaw
	}
}

// normalizeLines splits the raw logo text into lines, strips \r characters,
// drops trailing empty lines, and pads every line with spaces so that all
// lines share the same width.
func normalizeLines(raw string) []string {
	raw = strings.ReplaceAll(raw, "\r", "")
	lines := strings.Split(raw, "\n")

	// Trim trailing empty lines.
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}

	if len(lines) == 0 {
		return nil
	}

	// Find maximum width (in runes).
	maxWidth := 0
	for _, line := range lines {
		if w := len([]rune(line)); w > maxWidth {
			maxWidth = w
		}
	}

	// Pad each line to maxWidth.
	for i, line := range lines {
		runeLen := len([]rune(line))
		if runeLen < maxWidth {
			lines[i] = line + strings.Repeat(" ", maxWidth-runeLen)
		}
	}

	return lines
}
