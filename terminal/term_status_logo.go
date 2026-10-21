package terminal

import (
	"embed"
	"io/fs"
	"regexp"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/mclucy/lucy/terminal/style"
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

// FieldLogo is a Field that holds the ASCII logo for the status view.
// It also provides the line and width data used by the layout compositor.
type FieldLogo struct {
	Product   types.ServerProduct
	Ecosystem types.Ecosystem
	Version   types.Version
	NoColor   bool
	Mode      StatusLogoMode
}

// Render returns the selected logo as a string.
func (f *FieldLogo) Render() string {
	logo := GetLogo(f.Product, f.Ecosystem, f.Version, f.renderVariant())
	return strings.Join(normalizeLines(logo), "\n")
}

func (f *FieldLogo) renderVariant() LogoVariant {
	return f.renderVariantForWidth(style.TermWidth())
}

func (f *FieldLogo) renderVariantForWidth(termWidth int) LogoVariant {
	switch f.Mode {
	case StatusLogoLarge:
		return f.variant(true)
	case StatusLogoSmall:
		return f.variant(false)
	default:
		large := f.variant(true)
		return f.variant(termWidth >= f.Width(large)+statusLayoutGapWidth+statusLayoutMinInfoWidth)
	}
}

func (f *FieldLogo) variant(large bool) LogoVariant {
	if large {
		if f.NoColor {
			return LogoLargePlain
		}
		return LogoLargeColored
	}
	if f.NoColor {
		return LogoSmallPlain
	}
	return LogoSmallColored
}

// KeyLength returns 0 because the logo has no key.
func (f *FieldLogo) KeyLength() int {
	return 0
}

// Lines returns normalized lines for the requested logo variant.
func (f *FieldLogo) Lines(variant LogoVariant) []string {
	return normalizeLines(GetLogo(f.Product, f.Ecosystem, f.Version, variant))
}

// Width returns the visual width of the requested logo variant.
func (f *FieldLogo) Width(variant LogoVariant) int {
	lines := normalizeLines(GetLogo(f.Product, f.Ecosystem, f.Version, variant))
	if len(lines) == 0 {
		return 0
	}
	return lipgloss.Width(lines[0])
}

// Height returns the number of lines in the requested logo variant.
func (f *FieldLogo) Height(variant LogoVariant) int {
	return len(normalizeLines(GetLogo(f.Product, f.Ecosystem, f.Version, variant)))
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

// The logo assets are generated from assets/logos.yaml by tools/genlogo.
// Colored assets carry their own SGR escapes.
//
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
		assets[strings.TrimPrefix(path, "assets/")] = string(data)
		return nil
	})
	if err != nil {
		panic(err)
	}
	return assets
}

// GetLogo returns the logo for the server product, ecosystem, Minecraft
// version, and variant.
func GetLogo(
	product types.ServerProduct,
	ecosystem types.Ecosystem,
	minecraftVersion types.Version,
	variant LogoVariant,
) string {
	name := logoAssetName(product, ecosystem, minecraftVersion)
	if name == "" {
		return ""
	}
	directory, ok := logoVariantDirectory(variant)
	if !ok {
		return ""
	}
	return embeddedLogoAssets[directory+"/"+name+".txt"]
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
	product types.ServerProduct,
	ecosystem types.Ecosystem,
	minecraftVersion types.Version,
) string {
	name := strings.ToLower(strings.TrimSpace(product.String()))
	switch name {
	case "minecraft", "vanilla":
		if strings.Contains(strings.ToLower(minecraftVersion.String()), "snapshot") ||
			snapshotVersionPattern.MatchString(minecraftVersion.String()) {
			return "vanilla-snapshot"
		}
		return "vanilla"
	case "paper":
		return "papermc"
	case "mcdreforged":
		return "mcdr"
	case "bukkit", "craftbukkit":
		return "spigot"
	case "spongevanilla", "spongeforge", "spongeneo":
		return "sponge"
	}

	if embeddedLogoAssets["small_plain/"+name+".txt"] != "" {
		return name
	}

	switch ecosystem {
	case types.EcosystemFabric:
		return "fabric"
	case types.EcosystemForge:
		return "forge"
	case types.EcosystemNeoForge:
		return "neoforge"
	case types.EcosystemBukkit, types.EcosystemPaper:
		return "spigot"
	case types.EcosystemSponge:
		return "sponge"
	case types.EcosystemVelocity:
		return "velocity"
	case types.EcosystemMCDR:
		return "mcdr"
	default:
		return ""
	}
}

var snapshotVersionPattern = regexp.MustCompile(`^\d\dw\d+[a-z]$`)
