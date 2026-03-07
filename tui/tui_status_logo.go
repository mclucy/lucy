package tui

import (
	_ "embed"
	"strings"

	"github.com/mclucy/lucy/tools"
	"github.com/mclucy/lucy/types"
)

// LogoVariant selects between the large and small logo variants.
type LogoVariant int

const (
	// LogoLargePlain selects the full-size ASCII art logo.
	LogoLargePlain LogoVariant = iota
	// LogoSmallPlain selects the compact ASCII art logo.
	LogoSmallPlain
	LogoLargeColored
	LogoSmallColored
)

const (
	logoSmallMaxWidth = 30
	logoLargeMaxWidth = 72
)

// FieldLogo is a Field that holds the ASCII logo for the status view.
// It satisfies the Field interface so it can be placed in Data.Fields,
// but its primary API is the Lines / Width / Height helpers which the
// layout compositor uses to build the neofetch-style side-by-side view.
type FieldLogo struct {
	Platform types.Platform // TODO: this is not limited to platform
	NoColor  bool
}

// Render returns the large logo as a plain string. This is a fallback for
// callers that are not layout-aware and simply iterate over Fields.
func (f *FieldLogo) Render() string {
	variant := tools.Ternary(
		useLargeLogo(),
		tools.Ternary(f.NoColor, LogoLargePlain, LogoLargeColored),
		tools.Ternary(f.NoColor, LogoSmallPlain, LogoSmallColored),
	)
	logo := GetLogo(f.Platform, variant)
	return strings.Join(normalizeLines(logo), "\n")
}

// KeyLength returns 0 because the logo is not a key-value field.
func (f *FieldLogo) KeyLength() int {
	return 0
}

// Lines returns the normalized lines of the requested logo variant.
// Each line is padded with trailing spaces so that all lines share the
// same width, making grid-based composition straightforward.
func (f *FieldLogo) Lines(variant LogoVariant) []string {
	return normalizeLines(GetLogo(f.Platform, variant))
}

// Width returns the uniform width (in runes) of every line for the given
// logo variant.
func (f *FieldLogo) Width(variant LogoVariant) int {
	lines := normalizeLines(GetLogo(f.Platform, variant))
	if len(lines) == 0 {
		return 0
	}
	return len([]rune(lines[0]))
}

// Height returns the number of lines for the given logo variant.
func (f *FieldLogo) Height(variant LogoVariant) int {
	return len(normalizeLines(GetLogo(f.Platform, variant)))
}

func useLargeLogo() bool {
	termWidth := tools.TermWidth()
	return termWidth >= logoLargeMaxWidth+statusLayoutGapWidth+statusLayoutMinInfoWidth
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
