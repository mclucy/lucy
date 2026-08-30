package tui

import (
	"embed"
	"encoding/hex"
	"html"
	"io/fs"
	"regexp"
	"strconv"
	"strings"

	"github.com/mclucy/lucy/types"
)

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
