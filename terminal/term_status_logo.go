package terminal

import (
	"embed"
	"encoding/hex"
	"html"
	"io/fs"
	"regexp"
	"strconv"
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
// It also provides the line and width data used by the layout compositor.
type FieldLogo struct {
	Core    types.BarePackageName
	Eco     types.Ecosystem
	Version types.BareVersion
	NoColor bool
	Mode    StatusLogoMode
}

// Render returns the selected logo as a string.
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

// KeyLength returns 0 because the logo has no key.
func (f *FieldLogo) KeyLength() int {
	return 0
}

// Lines returns normalized lines for the requested logo variant.
func (f *FieldLogo) Lines(variant LogoVariant) []string {
	return normalizeLines(GetLogo(f.Core, f.Eco, f.Version, variant))
}

// Width returns the visual width of the requested logo variant.
func (f *FieldLogo) Width(variant LogoVariant) int {
	lines := normalizeLines(GetLogo(f.Core, f.Eco, f.Version, variant))
	if len(lines) == 0 {
		return 0
	}
	return lipgloss.Width(lines[0])
}

// Height returns the number of lines in the requested logo variant.
func (f *FieldLogo) Height(variant LogoVariant) int {
	return len(normalizeLines(GetLogo(f.Core, f.Eco, f.Version, variant)))
}

// normalizeLines removes empty edges and pads lines to the same visual width.
func normalizeLines(raw string) []string {
	raw = strings.ReplaceAll(raw, "\r", "")
	lines := strings.Split(raw, "\n")

	for len(lines) > 0 && strings.TrimSpace(ansi.Strip(lines[0])) == "" {
		lines = lines[1:]
	}

	for len(lines) > 0 && strings.TrimSpace(ansi.Strip(lines[len(lines)-1])) == "" {
		lines = lines[:len(lines)-1]
	}

	if len(lines) == 0 {
		return nil
	}

	maxWidth := 0
	for _, line := range lines {
		if width := lipgloss.Width(line); width > maxWidth {
			maxWidth = width
		}
	}

	for i, line := range lines {
		if width := lipgloss.Width(line); width < maxWidth {
			lines[i] = line + strings.Repeat(" ", maxWidth-width)
		}
	}

	return lines
}

//go:embed assets/large_plain/*.txt assets/small_plain/*.txt assets/large_colored/*.txt assets/small_colored/*.txt
var logoAssets embed.FS

var embeddedLogoAssets = loadEmbeddedLogoAssets()

func loadEmbeddedLogoAssets() map[string]string {
	assets := make(map[string]string)
	err := fs.WalkDir(logoAssets, "assets", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(logoAssets, path)
		if err != nil {
			return err
		}
		text := string(data)
		if strings.Contains(path, "_colored/") {
			text = convertColoredLogo(text)
		}
		assets[strings.TrimPrefix(path, "assets/")] = text
		return nil
	})
	if err != nil {
		panic(err)
	}
	return assets
}

// GetLogo returns the logo for the package, platform, version, and variant.
func GetLogo(
	core types.BarePackageName,
	platform types.Ecosystem,
	gameVersion types.BareVersion,
	variant LogoVariant,
) string {
	name := logoAssetName(core, platform, gameVersion)
	if name == "" {
		return ""
	}

	directory, ok := logoVariantDirectory(variant)
	if !ok {
		return ""
	}
	if logo, ok := embeddedLogoAssets[directory+"/"+name+".txt"]; ok {
		return logo
	}
	if variant == LogoLargeColored {
		return embeddedLogoAssets["large_plain/"+name+".txt"]
	}
	if variant == LogoSmallColored {
		return embeddedLogoAssets["small_plain/"+name+".txt"]
	}
	return ""
}

func logoVariantDirectory(variant LogoVariant) (string, bool) {
	switch variant {
	case LogoLargePlain:
		return "large_plain", true
	case LogoSmallPlain:
		return "small_plain", true
	case LogoLargeColored:
		return "large_colored", true
	case LogoSmallColored:
		return "small_colored", true
	default:
		return "", false
	}
}

func logoAssetName(
	core types.BarePackageName,
	platform types.Ecosystem,
	gameVersion types.BareVersion,
) string {
	name := strings.ToLower(strings.TrimSpace(core.String()))
	switch name {
	case "minecraft", "vanilla":
		if strings.Contains(strings.ToLower(gameVersion.String()), "snapshot") ||
			snapshotVersionPattern.MatchString(gameVersion.String()) {
			return "vanilla-snapshot"
		}
		return "vanilla"
	case "paper":
		return "papermc"
	case "bukkit", "craftbukkit":
		return "spigot"
	case "spongevanilla", "spongeforge", "spongeneo":
		return "sponge"
	}

	if embeddedLogoAssets["small_plain/"+name+".txt"] != "" {
		return name
	}

	switch platform {
	case types.EcoFabric:
		return "fabric"
	case types.EcoForge:
		return "forge"
	case types.EcoNeoforge:
		return "neoforge"
	case types.EcoBukkit, types.EcoPaper:
		return "spigot"
	case types.EcoSponge:
		return "sponge"
	case types.EcoVelocity:
		return "velocity"
	case types.EcoMcdr:
		return "mcdr"
	default:
		return ""
	}
}

var (
	coloredLogoTagPattern   = regexp.MustCompile(`\[/?(?:size|font)[^\]]*\]|<[^>]*>`)
	coloredLogoColorPattern = regexp.MustCompile(`(?s)\[color=#([0-9a-fA-F]{6})\](.*?)\[/color\]`)
)

func convertColoredLogo(raw string) string {
	raw = html.UnescapeString(raw)
	raw = coloredLogoColorPattern.ReplaceAllStringFunc(raw, func(match string) string {
		parts := coloredLogoColorPattern.FindStringSubmatch(match)
		color, err := hex.DecodeString(parts[1])
		if err != nil {
			return parts[2]
		}
		return "\x1b[38;2;" + strconv.Itoa(int(color[0])) + ";" +
			strconv.Itoa(int(color[1])) + ";" + strconv.Itoa(int(color[2])) +
			"m" + parts[2] + "\x1b[0m"
	})
	return coloredLogoTagPattern.ReplaceAllString(raw, "")
}

var snapshotVersionPattern = regexp.MustCompile(`^\d\dw\d+[a-z]$`)
