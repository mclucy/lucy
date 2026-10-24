package terminal

import (
	"embed"
	"io/fs"
	"regexp"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/version"
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
	product server.Product,
	ecosystem catalog.Ecosystem,
	minecraftVersion version.Version,
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
	product server.Product,
	ecosystem catalog.Ecosystem,
	minecraftVersion version.Version,
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
	case catalog.EcosystemFabric:
		return "fabric"
	case catalog.EcosystemForge:
		return "forge"
	case catalog.EcosystemNeoForge:
		return "neoforge"
	case catalog.EcosystemBukkit, catalog.EcosystemPaper:
		return "spigot"
	case catalog.EcosystemSponge:
		return "sponge"
	case catalog.EcosystemVelocity:
		return "velocity"
	case catalog.EcosystemMCDR:
		return "mcdr"
	default:
		return ""
	}
}

var snapshotVersionPattern = regexp.MustCompile(`^\d\dw\d+[a-z]$`)
