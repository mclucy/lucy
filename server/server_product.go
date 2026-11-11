package server

import (
	"slices"
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
	ProductQuilt            Product = "quilt"
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

// Ecosystem returns the package ecosystem p natively serves, or
// catalog.EcosystemUnspecified when p serves none on its own.
func (p Product) Ecosystem() catalog.Ecosystem {
	if definition, ok := productDefinitionOf(p); ok {
		return definition.Ecosystem
	}
	return catalog.EcosystemUnspecified
}

// Title returns the presentation name of p.
func (p Product) Title() string {
	if definition, ok := productDefinitionOf(p); ok {
		return definition.Title
	}
	return "Unknown"
}

// LoaderComponent returns the runtime component name p reports as a mod
// loader, or an empty name when p is not a mod loader.
func (p Product) LoaderComponent() catalog.Name {
	if definition, ok := productDefinitionOf(p); ok {
		return definition.Loader.Component
	}
	return ""
}

// LoaderEcosystem returns the ecosystem of the mod loader that spelling
// names, or catalog.EcosystemUnspecified. Only loader component spellings
// match; product aliases such as "paper" do not.
func LoaderEcosystem(spelling string) catalog.Ecosystem {
	name := catalog.Name(strings.ToLower(strings.TrimSpace(spelling)))
	for _, definition := range productDefinitions {
		if definition.Loader.recognizes(name) {
			return definition.Ecosystem
		}
	}
	return catalog.EcosystemUnspecified
}

// LoaderComponentName returns the component name that the mod loader of
// ecosystem reports for spelling. The second result reports whether spelling
// names that loader.
func LoaderComponentName(
	ecosystem catalog.Ecosystem,
	spelling string,
) (catalog.Name, bool) {
	name := catalog.Name(strings.ToLower(strings.TrimSpace(spelling)))
	for _, definition := range productDefinitions {
		if definition.Ecosystem != ecosystem || !definition.Loader.recognizes(name) {
			continue
		}
		return definition.Loader.Component, true
	}
	return "", false
}

func productDefinitionOf(p Product) (productDefinition, bool) {
	for _, definition := range productDefinitions {
		if definition.Product == p {
			return definition, true
		}
	}
	return productDefinition{}, false
}

type Match struct {
	Product   Product
	Ecosystem catalog.Ecosystem
}

type productAlias struct {
	Name      catalog.Name
	Ecosystem catalog.Ecosystem
}

// loaderDefinition names the runtime component a mod loader reports.
type loaderDefinition struct {
	// Component is the component name the loader reports.
	Component catalog.Name
	// Spellings are the other accepted names of that component, such as the
	// name without the hyphen.
	Spellings []catalog.Name
}

func (l loaderDefinition) recognizes(name catalog.Name) bool {
	if name == "" || l.Component == "" {
		return false
	}
	return name == l.Component || slices.Contains(l.Spellings, name)
}

type productDefinition struct {
	Product Product
	Title   string
	// Ecosystem is the package ecosystem the product serves on its own.
	Ecosystem catalog.Ecosystem
	// Loader is set only on mod loader products.
	Loader  loaderDefinition
	Aliases []productAlias
}

// productDefinitions maps accepted product spellings to canonical
// server products. Ecosystem disambiguates aliases such as Sponge's Forge
// variant. The table is also the single source of product titles and loader
// component names.
var productDefinitions = []productDefinition{
	{
		Product:   ProductMinecraft,
		Title:     "Minecraft",
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
		Title:     "Fabric",
		Ecosystem: catalog.EcosystemFabric,
		Loader: loaderDefinition{
			Component: "fabric-loader",
			Spellings: []catalog.Name{"fabric", "fabricloader"},
		},
		Aliases: []productAlias{
			{"fabric", catalog.EcosystemUnspecified},
			{"fabric-loader", catalog.EcosystemUnspecified},
			{"fabric", catalog.EcosystemFabric},
			{"fabric-loader", catalog.EcosystemFabric},
		},
	},
	{
		Product:   ProductQuilt,
		Title:     "Quilt",
		Ecosystem: catalog.EcosystemQuilt,
		Loader: loaderDefinition{
			Component: "quilt-loader",
			Spellings: []catalog.Name{"quilt", "quiltloader"},
		},
		Aliases: []productAlias{
			{"quilt", catalog.EcosystemUnspecified},
			{"quilt-loader", catalog.EcosystemUnspecified},
			{"quilt", catalog.EcosystemQuilt},
			{"quilt-loader", catalog.EcosystemQuilt},
		},
	},
	{
		Product:   ProductForge,
		Title:     "Forge",
		Ecosystem: catalog.EcosystemForge,
		Loader: loaderDefinition{
			Component: "forge",
		},
		Aliases: []productAlias{
			{"forge", catalog.EcosystemUnspecified},
			{"forge", catalog.EcosystemForge},
		},
	},
	{
		Product:   ProductNeoForge,
		Title:     "NeoForge",
		Ecosystem: catalog.EcosystemNeoForge,
		Loader: loaderDefinition{
			Component: "neoforge",
		},
		Aliases: []productAlias{
			{"neoforge", catalog.EcosystemUnspecified},
			{"neoforge", catalog.EcosystemNeoForge},
		},
	},
	{
		Product:   ProductMCDReforged,
		Title:     "MCDReforged",
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
		Title:     "CraftBukkit",
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
		Title:     "Spigot",
		Ecosystem: catalog.EcosystemBukkit,
		Aliases: []productAlias{
			{"spigot", catalog.EcosystemUnspecified},
			{"spigot", catalog.EcosystemBukkit},
		},
	},
	{
		Product:   ProductPaper,
		Title:     "Paper",
		Ecosystem: catalog.EcosystemPaper,
		Aliases: []productAlias{
			{"paper", catalog.EcosystemUnspecified},
			{"paper", catalog.EcosystemPaper},
		},
	},
	{
		Product:   ProductPurpur,
		Title:     "Purpur",
		Ecosystem: catalog.EcosystemPaper,
		Aliases: []productAlias{
			{"purpur", catalog.EcosystemUnspecified},
			{"purpur", catalog.EcosystemPaper},
		},
	},
	{
		Product:   ProductFolia,
		Title:     "Folia",
		Ecosystem: catalog.EcosystemPaper,
		Aliases: []productAlias{
			{"folia", catalog.EcosystemUnspecified},
			{"folia", catalog.EcosystemPaper},
		},
	},
	{
		Product:   ProductLeaves,
		Title:     "Leaves",
		Ecosystem: catalog.EcosystemPaper,
		Aliases: []productAlias{
			{"leaves", catalog.EcosystemUnspecified},
			{"leaves", catalog.EcosystemPaper},
		},
	},
	{
		Product: ProductArclight,
		Title:   "Arclight",
		Aliases: []productAlias{{"arclight", catalog.EcosystemUnspecified}},
	},
	{
		Product: ProductArclightForge,
		Title:   "Arclight Forge",
		Aliases: []productAlias{{"arclight-forge", catalog.EcosystemUnspecified}},
	},
	{
		Product: ProductArclightNeoForge,
		Title:   "Arclight NeoForge",
		Aliases: []productAlias{
			{
				"arclight-neoforge",
				catalog.EcosystemUnspecified,
			},
		},
	},
	{
		Product: ProductArclightFabric,
		Title:   "Arclight Fabric",
		Aliases: []productAlias{{"arclight-fabric", catalog.EcosystemUnspecified}},
	},
	{
		Product: ProductCatServer,
		Title:   "CatServer",
		Aliases: []productAlias{{"catserver", catalog.EcosystemUnspecified}},
	},
	{
		Product: ProductYouer,
		Title:   "Youer",
		Aliases: []productAlias{{"youer", catalog.EcosystemUnspecified}},
	},
	{
		Product:   ProductSpongeVanilla,
		Title:     "SpongeVanilla",
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
		Title:     "SpongeForge",
		Ecosystem: catalog.EcosystemSponge,
		Aliases: []productAlias{
			{"spongeforge", catalog.EcosystemUnspecified},
			{"spongeforge", catalog.EcosystemSponge},
			{"forge", catalog.EcosystemSponge},
		},
	},
	{
		Product:   ProductSpongeNeo,
		Title:     "SpongeNeo",
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
		Title:     "BungeeCord",
		Ecosystem: catalog.EcosystemBungeeCord,
		Aliases: []productAlias{
			{"bungeecord", catalog.EcosystemUnspecified},
			{"bungeecord", catalog.EcosystemBungeeCord},
		},
	},
	{
		Product:   ProductVelocity,
		Title:     "Velocity",
		Ecosystem: catalog.EcosystemVelocity,
		Aliases: []productAlias{
			{"velocity", catalog.EcosystemUnspecified},
			{"velocity", catalog.EcosystemVelocity},
		},
	},
	{
		Product:   ProductWaterfall,
		Title:     "Waterfall",
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
