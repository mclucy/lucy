package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/mclucy/lucy/internal/fn"
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
	Core    types.BarePackageName
	Eco     types.Ecosystem
	Version types.BareVersion
	NoColor bool
	Mode    StatusLogoMode
}

// Render returns the large logo as a plain string. This is a fallback for
// callers that are not layout-aware and simply iterate over Fields.
func (f *FieldLogo) Render() string {
	logo := GetLogo(f.Core, f.Eco, f.Version, f.renderVariant())
	return strings.Join(normalizeLines(logo), "\n")
}

func (f *FieldLogo) renderVariant() LogoVariant {
	if f.Mode == StatusLogoLarge {
		return fn.Ternary(f.NoColor, LogoLargePlain, LogoLargeColored)
	}
	return fn.Ternary(f.NoColor, LogoSmallPlain, LogoSmallColored)
}

// KeyLength returns 0 because the logo is not a key-value field.
func (f *FieldLogo) KeyLength() int {
	return 0
}

// Lines returns the normalized lines of the requested logo variant.
// Each line is padded with trailing spaces so that all lines share the
// same width, making grid-based composition straightforward.
func (f *FieldLogo) Lines(variant LogoVariant) []string {
	return normalizeLines(GetLogo(f.Core, f.Eco, f.Version, variant))
}

// Width returns the uniform width (in runes) of every line for the given
// logo variant.
func (f *FieldLogo) Width(variant LogoVariant) int {
	lines := normalizeLines(GetLogo(f.Core, f.Eco, f.Version, variant))
	if len(lines) == 0 {
		return 0
	}
	return lipgloss.Width(lines[0])
}

// Height returns the number of lines for the given logo variant.
func (f *FieldLogo) Height(variant LogoVariant) int {
	return len(normalizeLines(GetLogo(f.Core, f.Eco, f.Version, variant)))
}

// normalizeLines splits the raw logo text into lines, strips \r characters,
// drops trailing empty lines, and pads every line with spaces so that all
// lines share the same width.
func normalizeLines(raw string) []string {
	raw = strings.ReplaceAll(raw, "\r", "")
	lines := strings.Split(raw, "\n")

	for len(lines) > 0 && strings.TrimSpace(ansi.Strip(lines[0])) == "" {
		lines = lines[1:]
	}

	// Trim trailing empty lines.
	for len(lines) > 0 && strings.TrimSpace(ansi.Strip(lines[len(lines)-1])) == "" {
		lines = lines[:len(lines)-1]
	}

	if len(lines) == 0 {
		return nil
	}

	// Find maximum visual width; ANSI escape sequences occupy no columns.
	maxWidth := 0
	for _, line := range lines {
		if w := lipgloss.Width(line); w > maxWidth {
			maxWidth = w
		}
	}

	// Pad each line to maxWidth using visual width.
	for i, line := range lines {
		if width := lipgloss.Width(line); width < maxWidth {
			lines[i] = line + strings.Repeat(" ", maxWidth-width)
		}
	}

	return lines
}
