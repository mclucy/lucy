package server

import (
	"strings"

	"github.com/mclucy/lucy/catalog"
)

// Product identifies the executable server product. It is the ground
// fact about what the server runs; an Ecosystem describes what it can serve.
type Product string

const (
	ProductUnknown Product = ""

	ProductMinecraft        Product = "minecraft"
	ProductFabric           Product = "fabric"
	ProductForge            Product = "forge"
	ProductNeoForge         Product = "neoforge"
	ProductMCDReforged      Product = "mcdreforged"
	ProductCraftBukkit      Product = "craftbukkit"
	ProductSpigot           Product = "spigot"
	ProductPaper            Product = "paper"
	ProductPurpur           Product = "purpur"
	ProductFolia            Product = "folia"
	ProductLeaves           Product = "leaves"
	ProductArclight         Product = "arclight"
	ProductArclightForge    Product = "arclight-forge"
	ProductArclightNeoForge Product = "arclight-neoforge"
	ProductArclightFabric   Product = "arclight-fabric"
	ProductCatServer        Product = "catserver"
	ProductYouer            Product = "youer"
	ProductSpongeVanilla    Product = "spongevanilla"
	ProductSpongeForge      Product = "spongeforge"
	ProductSpongeNeo        Product = "spongeneo"
	ProductBungeeCord       Product = "bungeecord"
	ProductVelocity         Product = "velocity"
	ProductWaterfall        Product = "waterfall"
)

func (p Product) String() string {
	return string(p)
}

func (p Product) Title() string {
	switch p {
	case ProductMinecraft:
		return "Minecraft"
	case ProductFabric:
		return "Fabric"
	case ProductForge:
		return "Forge"
	case ProductNeoForge:
		return "NeoForge"
	case ProductMCDReforged:
		return "MCDReforged"
	case ProductCraftBukkit:
		return "CraftBukkit"
	case ProductSpigot:
		return "Spigot"
	case ProductPaper:
		return "Paper"
	case ProductPurpur:
		return "Purpur"
	case ProductFolia:
		return "Folia"
	case ProductLeaves:
		return "Leaves"
	case ProductArclight:
		return "Arclight"
	case ProductArclightForge:
		return "Arclight Forge"
	case ProductArclightNeoForge:
		return "Arclight NeoForge"
	case ProductArclightFabric:
		return "Arclight Fabric"
	case ProductCatServer:
		return "CatServer"
	case ProductYouer:
		return "Youer"
	case ProductSpongeVanilla:
		return "SpongeVanilla"
	case ProductSpongeForge:
		return "SpongeForge"
	case ProductSpongeNeo:
		return "SpongeNeo"
	case ProductBungeeCord:
		return "BungeeCord"
	case ProductVelocity:
		return "Velocity"
	case ProductWaterfall:
		return "Waterfall"
	default:
		return "Unknown"
	}
}

type Match struct {
	Product   Product
	Ecosystem catalog.Ecosystem
}

type productAlias struct {
	Name      catalog.Name
	Ecosystem catalog.Ecosystem
}

type productDefinition struct {
	Product   Product
	Ecosystem catalog.Ecosystem
	Aliases   []productAlias
}

// productDefinitions maps accepted product spellings to canonical
// server products. Ecosystem disambiguates aliases such as Sponge's Forge
// variant.
var productDefinitions = []productDefinition{
	{
		Product:   ProductMinecraft,
		Ecosystem: catalog.EcosystemMinecraft,
		Aliases: []productAlias{
			{"minecraft", catalog.EcosystemUnspecified},
			{"mc", catalog.EcosystemUnspecified},
			{"vanilla", catalog.EcosystemUnspecified},
			{"minecraft", catalog.EcosystemMinecraft},
			{"mc", catalog.EcosystemMinecraft},
			{"vanilla", catalog.EcosystemMinecraft},
		},
	},
	{
		Product:   ProductFabric,
		Ecosystem: catalog.EcosystemFabric,
		Aliases: []productAlias{
			{"fabric", catalog.EcosystemUnspecified},
			{"fabric-loader", catalog.EcosystemUnspecified},
			{"fabric", catalog.EcosystemFabric},
			{"fabric-loader", catalog.EcosystemFabric},
		},
	},
	{
		Product:   ProductForge,
		Ecosystem: catalog.EcosystemForge,
		Aliases: []productAlias{
			{"forge", catalog.EcosystemUnspecified},
			{"forge", catalog.EcosystemForge},
		},
	},
	{
		Product:   ProductNeoForge,
		Ecosystem: catalog.EcosystemNeoForge,
		Aliases: []productAlias{
			{"neoforge", catalog.EcosystemUnspecified},
			{"neoforge", catalog.EcosystemNeoForge},
		},
	},
	{
		Product:   ProductMCDReforged,
		Ecosystem: catalog.EcosystemMCDR,
		Aliases: []productAlias{
			{"mcdreforged", catalog.EcosystemUnspecified},
			{"mcdr", catalog.EcosystemUnspecified},
			{"mcdreforged", catalog.EcosystemMCDR},
			{"mcdr", catalog.EcosystemMCDR},
		},
	},
	{
		Product:   ProductCraftBukkit,
		Ecosystem: catalog.EcosystemBukkit,
		Aliases: []productAlias{
			{"bukkit", catalog.EcosystemUnspecified},
			{"craftbukkit", catalog.EcosystemUnspecified},
			{"bukkit", catalog.EcosystemBukkit},
			{"craftbukkit", catalog.EcosystemBukkit},
		},
	},
	{
		Product:   ProductSpigot,
		Ecosystem: catalog.EcosystemBukkit,
		Aliases: []productAlias{
			{"spigot", catalog.EcosystemUnspecified},
			{"spigot", catalog.EcosystemBukkit},
		},
	},
	{
		Product:   ProductPaper,
		Ecosystem: catalog.EcosystemPaper,
		Aliases: []productAlias{
			{"paper", catalog.EcosystemUnspecified},
			{"paper", catalog.EcosystemPaper},
		},
	},
	{
		Product:   ProductPurpur,
		Ecosystem: catalog.EcosystemPaper,
		Aliases: []productAlias{
			{"purpur", catalog.EcosystemUnspecified},
			{"purpur", catalog.EcosystemPaper},
		},
	},
	{
		Product:   ProductFolia,
		Ecosystem: catalog.EcosystemPaper,
		Aliases: []productAlias{
			{"folia", catalog.EcosystemUnspecified},
			{"folia", catalog.EcosystemPaper},
		},
	},
	{
		Product:   ProductLeaves,
		Ecosystem: catalog.EcosystemPaper,
		Aliases: []productAlias{
			{"leaves", catalog.EcosystemUnspecified},
			{"leaves", catalog.EcosystemPaper},
		},
	},
	{
		Product: ProductArclight,
		Aliases: []productAlias{{"arclight", catalog.EcosystemUnspecified}},
	},
	{
		Product: ProductArclightForge,
		Aliases: []productAlias{{"arclight-forge", catalog.EcosystemUnspecified}},
	},
	{
		Product: ProductArclightNeoForge,
		Aliases: []productAlias{
			{
				"arclight-neoforge",
				catalog.EcosystemUnspecified,
			},
		},
	},
	{
		Product: ProductArclightFabric,
		Aliases: []productAlias{{"arclight-fabric", catalog.EcosystemUnspecified}},
	},
	{
		Product: ProductCatServer,
		Aliases: []productAlias{{"catserver", catalog.EcosystemUnspecified}},
	},
	{
		Product: ProductYouer,
		Aliases: []productAlias{{"youer", catalog.EcosystemUnspecified}},
	},
	{
		Product:   ProductSpongeVanilla,
		Ecosystem: catalog.EcosystemSponge,
		Aliases: []productAlias{
			{"sponge", catalog.EcosystemUnspecified},
			{"spongevanilla", catalog.EcosystemUnspecified},
			{"sponge", catalog.EcosystemSponge},
			{"spongevanilla", catalog.EcosystemSponge},
			{"vanilla", catalog.EcosystemSponge},
			{"minecraft", catalog.EcosystemSponge},
			{"mc", catalog.EcosystemSponge},
		},
	},
	{
		Product:   ProductSpongeForge,
		Ecosystem: catalog.EcosystemSponge,
		Aliases: []productAlias{
			{"spongeforge", catalog.EcosystemUnspecified},
			{"spongeforge", catalog.EcosystemSponge},
			{"forge", catalog.EcosystemSponge},
		},
	},
	{
		Product:   ProductSpongeNeo,
		Ecosystem: catalog.EcosystemSponge,
		Aliases: []productAlias{
			{"spongeneo", catalog.EcosystemUnspecified},
			{"spongeneo", catalog.EcosystemSponge},
			{"neo", catalog.EcosystemSponge},
			{"neoforge", catalog.EcosystemSponge},
		},
	},
	{
		Product:   ProductBungeeCord,
		Ecosystem: catalog.EcosystemBungeeCord,
		Aliases: []productAlias{
			{"bungeecord", catalog.EcosystemUnspecified},
			{"bungeecord", catalog.EcosystemBungeeCord},
		},
	},
	{
		Product:   ProductVelocity,
		Ecosystem: catalog.EcosystemVelocity,
		Aliases: []productAlias{
			{"velocity", catalog.EcosystemUnspecified},
			{"velocity", catalog.EcosystemVelocity},
		},
	},
	{
		Product:   ProductWaterfall,
		Ecosystem: catalog.EcosystemBungeeCord,
		Aliases: []productAlias{
			{"waterfall", catalog.EcosystemUnspecified},
			{"waterfall", catalog.EcosystemBungeeCord},
		},
	},
}

// NormalizeProduct resolves a request against the server product
// catalog. Provider is intentionally not considered.
func NormalizeProduct(request catalog.VersionedRef) (Match, bool) {
	name := catalog.Name(strings.ToLower(request.Name.String()))
	for _, definition := range productDefinitions {
		for _, alias := range definition.Aliases {
			if alias.Name == name && alias.Ecosystem == request.Ecosystem {
				return Match{
					Product:   definition.Product,
					Ecosystem: definition.Ecosystem,
				}, true
			}
		}
	}
	return Match{}, false
}

// ParseProduct resolves a product spelling without an ecosystem
// qualifier. It is used for persisted server declarations.
func ParseProduct(name string) (Product, bool) {
	match, ok := NormalizeProduct(catalog.VersionedRef{
		Name: catalog.Name(strings.ToLower(strings.TrimSpace(name))),
	})
	return match.Product, ok
}

func IsProduct(request catalog.VersionedRef) bool {
	_, ok := NormalizeProduct(request)
	return ok
}
