package types

import "strings"

// ServerProduct identifies the executable server product. It is the ground
// fact about what the server runs; an Ecosystem describes what it can serve.
type ServerProduct string

const (
	ServerProductUnknown ServerProduct = ""

	ServerProductMinecraft        ServerProduct = "minecraft"
	ServerProductFabric           ServerProduct = "fabric"
	ServerProductForge            ServerProduct = "forge"
	ServerProductNeoForge         ServerProduct = "neoforge"
	ServerProductMCDReforged      ServerProduct = "mcdreforged"
	ServerProductCraftBukkit      ServerProduct = "craftbukkit"
	ServerProductSpigot           ServerProduct = "spigot"
	ServerProductPaper            ServerProduct = "paper"
	ServerProductPurpur           ServerProduct = "purpur"
	ServerProductFolia            ServerProduct = "folia"
	ServerProductLeaves           ServerProduct = "leaves"
	ServerProductArclight         ServerProduct = "arclight"
	ServerProductArclightForge    ServerProduct = "arclight-forge"
	ServerProductArclightNeoForge ServerProduct = "arclight-neoforge"
	ServerProductArclightFabric   ServerProduct = "arclight-fabric"
	ServerProductCatServer        ServerProduct = "catserver"
	ServerProductYouer            ServerProduct = "youer"
	ServerProductSpongeVanilla    ServerProduct = "spongevanilla"
	ServerProductSpongeForge      ServerProduct = "spongeforge"
	ServerProductSpongeNeo        ServerProduct = "spongeneo"
	ServerProductBungeeCord       ServerProduct = "bungeecord"
	ServerProductVelocity         ServerProduct = "velocity"
	ServerProductWaterfall        ServerProduct = "waterfall"
)

func (p ServerProduct) String() string {
	return string(p)
}

func (p ServerProduct) Title() string {
	switch p {
	case ServerProductMinecraft:
		return "Minecraft"
	case ServerProductFabric:
		return "Fabric"
	case ServerProductForge:
		return "Forge"
	case ServerProductNeoForge:
		return "NeoForge"
	case ServerProductMCDReforged:
		return "MCDReforged"
	case ServerProductCraftBukkit:
		return "CraftBukkit"
	case ServerProductSpigot:
		return "Spigot"
	case ServerProductPaper:
		return "Paper"
	case ServerProductPurpur:
		return "Purpur"
	case ServerProductFolia:
		return "Folia"
	case ServerProductLeaves:
		return "Leaves"
	case ServerProductArclight:
		return "Arclight"
	case ServerProductArclightForge:
		return "Arclight Forge"
	case ServerProductArclightNeoForge:
		return "Arclight NeoForge"
	case ServerProductArclightFabric:
		return "Arclight Fabric"
	case ServerProductCatServer:
		return "CatServer"
	case ServerProductYouer:
		return "Youer"
	case ServerProductSpongeVanilla:
		return "SpongeVanilla"
	case ServerProductSpongeForge:
		return "SpongeForge"
	case ServerProductSpongeNeo:
		return "SpongeNeo"
	case ServerProductBungeeCord:
		return "BungeeCord"
	case ServerProductVelocity:
		return "Velocity"
	case ServerProductWaterfall:
		return "Waterfall"
	default:
		return "Unknown"
	}
}

type ServerProductMatch struct {
	Product   ServerProduct
	Ecosystem Ecosystem
}

type serverProductAlias struct {
	Name      PackageName
	Ecosystem Ecosystem
}

type serverProductDefinition struct {
	Product   ServerProduct
	Ecosystem Ecosystem
	Aliases   []serverProductAlias
}

// serverProductDefinitions maps accepted product spellings to canonical
// server products. Ecosystem disambiguates aliases such as Sponge's Forge
// variant.
var serverProductDefinitions = []serverProductDefinition{
	{
		Product:   ServerProductMinecraft,
		Ecosystem: EcosystemMinecraft,
		Aliases: []serverProductAlias{
			{"minecraft", EcosystemUnspecified},
			{"mc", EcosystemUnspecified},
			{"vanilla", EcosystemUnspecified},
			{"minecraft", EcosystemMinecraft},
			{"mc", EcosystemMinecraft},
			{"vanilla", EcosystemMinecraft},
		},
	},
	{
		Product:   ServerProductFabric,
		Ecosystem: EcosystemFabric,
		Aliases: []serverProductAlias{
			{"fabric", EcosystemUnspecified},
			{"fabric-loader", EcosystemUnspecified},
			{"fabric", EcosystemFabric},
			{"fabric-loader", EcosystemFabric},
		},
	},
	{
		Product:   ServerProductForge,
		Ecosystem: EcosystemForge,
		Aliases: []serverProductAlias{
			{"forge", EcosystemUnspecified},
			{"forge", EcosystemForge},
		},
	},
	{
		Product:   ServerProductNeoForge,
		Ecosystem: EcosystemNeoForge,
		Aliases: []serverProductAlias{
			{"neoforge", EcosystemUnspecified},
			{"neoforge", EcosystemNeoForge},
		},
	},
	{
		Product:   ServerProductMCDReforged,
		Ecosystem: EcosystemMCDR,
		Aliases: []serverProductAlias{
			{"mcdreforged", EcosystemUnspecified},
			{"mcdr", EcosystemUnspecified},
			{"mcdreforged", EcosystemMCDR},
			{"mcdr", EcosystemMCDR},
		},
	},
	{
		Product:   ServerProductCraftBukkit,
		Ecosystem: EcosystemBukkit,
		Aliases: []serverProductAlias{
			{"bukkit", EcosystemUnspecified},
			{"craftbukkit", EcosystemUnspecified},
			{"bukkit", EcosystemBukkit},
			{"craftbukkit", EcosystemBukkit},
		},
	},
	{
		Product:   ServerProductSpigot,
		Ecosystem: EcosystemBukkit,
		Aliases: []serverProductAlias{
			{"spigot", EcosystemUnspecified},
			{"spigot", EcosystemBukkit},
		},
	},
	{
		Product:   ServerProductPaper,
		Ecosystem: EcosystemPaper,
		Aliases: []serverProductAlias{
			{"paper", EcosystemUnspecified},
			{"paper", EcosystemPaper},
		},
	},
	{
		Product:   ServerProductPurpur,
		Ecosystem: EcosystemPaper,
		Aliases: []serverProductAlias{
			{"purpur", EcosystemUnspecified},
			{"purpur", EcosystemPaper},
		},
	},
	{
		Product:   ServerProductFolia,
		Ecosystem: EcosystemPaper,
		Aliases: []serverProductAlias{
			{"folia", EcosystemUnspecified},
			{"folia", EcosystemPaper},
		},
	},
	{
		Product:   ServerProductLeaves,
		Ecosystem: EcosystemPaper,
		Aliases: []serverProductAlias{
			{"leaves", EcosystemUnspecified},
			{"leaves", EcosystemPaper},
		},
	},
	{
		Product: ServerProductArclight,
		Aliases: []serverProductAlias{{"arclight", EcosystemUnspecified}},
	},
	{
		Product: ServerProductArclightForge,
		Aliases: []serverProductAlias{{"arclight-forge", EcosystemUnspecified}},
	},
	{
		Product: ServerProductArclightNeoForge,
		Aliases: []serverProductAlias{
			{
				"arclight-neoforge",
				EcosystemUnspecified,
			},
		},
	},
	{
		Product: ServerProductArclightFabric,
		Aliases: []serverProductAlias{{"arclight-fabric", EcosystemUnspecified}},
	},
	{
		Product: ServerProductCatServer,
		Aliases: []serverProductAlias{{"catserver", EcosystemUnspecified}},
	},
	{
		Product: ServerProductYouer,
		Aliases: []serverProductAlias{{"youer", EcosystemUnspecified}},
	},
	{
		Product:   ServerProductSpongeVanilla,
		Ecosystem: EcosystemSponge,
		Aliases: []serverProductAlias{
			{"sponge", EcosystemUnspecified},
			{"spongevanilla", EcosystemUnspecified},
			{"sponge", EcosystemSponge},
			{"spongevanilla", EcosystemSponge},
			{"vanilla", EcosystemSponge},
			{"minecraft", EcosystemSponge},
			{"mc", EcosystemSponge},
		},
	},
	{
		Product:   ServerProductSpongeForge,
		Ecosystem: EcosystemSponge,
		Aliases: []serverProductAlias{
			{"spongeforge", EcosystemUnspecified},
			{"spongeforge", EcosystemSponge},
			{"forge", EcosystemSponge},
		},
	},
	{
		Product:   ServerProductSpongeNeo,
		Ecosystem: EcosystemSponge,
		Aliases: []serverProductAlias{
			{"spongeneo", EcosystemUnspecified},
			{"spongeneo", EcosystemSponge},
			{"neo", EcosystemSponge},
			{"neoforge", EcosystemSponge},
		},
	},
	{
		Product:   ServerProductBungeeCord,
		Ecosystem: EcosystemBungeeCord,
		Aliases: []serverProductAlias{
			{"bungeecord", EcosystemUnspecified},
			{"bungeecord", EcosystemBungeeCord},
		},
	},
	{
		Product:   ServerProductVelocity,
		Ecosystem: EcosystemVelocity,
		Aliases: []serverProductAlias{
			{"velocity", EcosystemUnspecified},
			{"velocity", EcosystemVelocity},
		},
	},
	{
		Product:   ServerProductWaterfall,
		Ecosystem: EcosystemBungeeCord,
		Aliases: []serverProductAlias{
			{"waterfall", EcosystemUnspecified},
			{"waterfall", EcosystemBungeeCord},
		},
	},
}

// NormalizeServerProduct resolves a request against the server product
// catalog. Provider is intentionally not considered.
func NormalizeServerProduct(request VersionedPackageRef) (ServerProductMatch, bool) {
	name := PackageName(strings.ToLower(request.Name.String()))
	for _, definition := range serverProductDefinitions {
		for _, alias := range definition.Aliases {
			if alias.Name == name && alias.Ecosystem == request.Ecosystem {
				return ServerProductMatch{
					Product:   definition.Product,
					Ecosystem: definition.Ecosystem,
				}, true
			}
		}
	}
	return ServerProductMatch{}, false
}

// ParseServerProduct resolves a product spelling without an ecosystem
// qualifier. It is used for persisted server declarations.
func ParseServerProduct(name string) (ServerProduct, bool) {
	match, ok := NormalizeServerProduct(VersionedPackageRef{
		Name: PackageName(strings.ToLower(strings.TrimSpace(name))),
	})
	return match.Product, ok
}

func IsServerProduct(request VersionedPackageRef) bool {
	_, ok := NormalizeServerProduct(request)
	return ok
}
