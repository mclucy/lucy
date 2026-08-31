package types

import "strings"

// CorePackage identifies a special request for a bootable server product or
// platform installer. It classifies requests; installation policy belongs to
// install and bootstrap.
type CorePackage string

const (
	CoreMinecraft        CorePackage = "minecraft"
	CoreFabric           CorePackage = "fabric"
	CoreForge            CorePackage = "forge"
	CoreNeoForge         CorePackage = "neoforge"
	CoreMCDReforged      CorePackage = "mcdreforged"
	CoreCraftBukkit      CorePackage = "craftbukkit"
	CoreSpigot           CorePackage = "spigot"
	CorePaper            CorePackage = "paper"
	CoreFolia            CorePackage = "folia"
	CoreLeaves           CorePackage = "leaves"
	CoreArclight         CorePackage = "arclight"
	CoreArclightForge    CorePackage = "arclight-forge"
	CoreArclightNeoForge CorePackage = "arclight-neoforge"
	CoreArclightFabric   CorePackage = "arclight-fabric"
	CoreCatServer        CorePackage = "catserver"
	CoreYouer            CorePackage = "youer"
	CoreSpongeVanilla    CorePackage = "spongevanilla"
	CoreSpongeForge      CorePackage = "spongeforge"
	CoreSpongeNeo        CorePackage = "spongeneo"
	CoreBungeeCord       CorePackage = "bungeecord"
	CoreVelocity         CorePackage = "velocity"
	CoreWaterfall        CorePackage = "waterfall"
)

type CorePackageMatch struct {
	Core CorePackage
	Eco  Ecosystem
}

type corePackageAlias struct {
	Name BarePackageName
	Eco  Ecosystem
}

type corePackageDefinition struct {
	Core    CorePackage
	Eco     Ecosystem
	Aliases []corePackageAlias
}

// corePackageDefinitions maps accepted core spellings to canonical core
// products. Eco disambiguates aliases such as Sponge's Forge variant.
var corePackageDefinitions = []corePackageDefinition{
	{Core: CoreMinecraft, Eco: EcoMinecraft, Aliases: []corePackageAlias{{"minecraft", EcoUnspecified}, {"mc", EcoUnspecified}, {"minecraft", EcoMinecraft}, {"mc", EcoMinecraft}}},
	{Core: CoreFabric, Eco: EcoFabric, Aliases: []corePackageAlias{{"fabric", EcoUnspecified}, {"fabric-loader", EcoUnspecified}, {"fabric", EcoFabric}, {"fabric-loader", EcoFabric}}},
	{Core: CoreForge, Eco: EcoForge, Aliases: []corePackageAlias{{"forge", EcoUnspecified}, {"forge", EcoForge}}},
	{Core: CoreNeoForge, Eco: EcoNeoforge, Aliases: []corePackageAlias{{"neoforge", EcoUnspecified}, {"neoforge", EcoNeoforge}}},
	{Core: CoreMCDReforged, Eco: EcoMcdr, Aliases: []corePackageAlias{{"mcdreforged", EcoUnspecified}, {"mcdr", EcoUnspecified}, {"mcdreforged", EcoMcdr}, {"mcdr", EcoMcdr}}},
	{Core: CoreCraftBukkit, Eco: EcoBukkit, Aliases: []corePackageAlias{{"bukkit", EcoUnspecified}, {"craftbukkit", EcoUnspecified}, {"bukkit", EcoBukkit}, {"craftbukkit", EcoBukkit}}},
	{Core: CoreSpigot, Eco: EcoBukkit, Aliases: []corePackageAlias{{"spigot", EcoUnspecified}, {"spigot", EcoBukkit}}},
	{Core: CorePaper, Eco: EcoPaper, Aliases: []corePackageAlias{{"paper", EcoUnspecified}, {"paper", EcoPaper}}},
	{Core: CoreFolia, Eco: EcoPaper, Aliases: []corePackageAlias{{"folia", EcoUnspecified}, {"folia", EcoPaper}}},
	{Core: CoreLeaves, Eco: EcoPaper, Aliases: []corePackageAlias{{"leaves", EcoUnspecified}, {"leaves", EcoPaper}}},
	{Core: CoreArclight, Aliases: []corePackageAlias{{"arclight", EcoUnspecified}}},
	{Core: CoreArclightForge, Aliases: []corePackageAlias{{"arclight-forge", EcoUnspecified}}},
	{Core: CoreArclightNeoForge, Aliases: []corePackageAlias{{"arclight-neoforge", EcoUnspecified}}},
	{Core: CoreArclightFabric, Aliases: []corePackageAlias{{"arclight-fabric", EcoUnspecified}}},
	{Core: CoreCatServer, Aliases: []corePackageAlias{{"catserver", EcoUnspecified}}},
	{Core: CoreYouer, Aliases: []corePackageAlias{{"youer", EcoUnspecified}}},
	{Core: CoreSpongeVanilla, Eco: EcoSponge, Aliases: []corePackageAlias{{"sponge", EcoUnspecified}, {"spongevanilla", EcoUnspecified}, {"sponge", EcoSponge}, {"spongevanilla", EcoSponge}, {"vanilla", EcoSponge}, {"minecraft", EcoSponge}, {"mc", EcoSponge}}},
	{Core: CoreSpongeForge, Eco: EcoSponge, Aliases: []corePackageAlias{{"spongeforge", EcoUnspecified}, {"spongeforge", EcoSponge}, {"forge", EcoSponge}}},
	{Core: CoreSpongeNeo, Eco: EcoSponge, Aliases: []corePackageAlias{{"spongeneo", EcoUnspecified}, {"spongeneo", EcoSponge}, {"neo", EcoSponge}, {"neoforge", EcoSponge}}},
	{Core: CoreBungeeCord, Eco: EcoBungeecord, Aliases: []corePackageAlias{{"bungeecord", EcoUnspecified}, {"bungeecord", EcoBungeecord}}},
	{Core: CoreVelocity, Eco: EcoVelocity, Aliases: []corePackageAlias{{"velocity", EcoUnspecified}, {"velocity", EcoVelocity}}},
	{Core: CoreWaterfall, Eco: EcoBungeecord, Aliases: []corePackageAlias{{"waterfall", EcoUnspecified}, {"waterfall", EcoBungeecord}}},
}

// NormalizeCorePackage resolves a request against the core catalog. Source is
// intentionally not considered: it is a provider-routing decision.
func NormalizeCorePackage(request PackageRequest) (CorePackageMatch, bool) {
	name := lowercasePackageName(request.Name)
	for _, definition := range corePackageDefinitions {
		for _, alias := range definition.Aliases {
			if alias.Name == name && alias.Eco == request.Eco {
				return CorePackageMatch{Core: definition.Core, Eco: definition.Eco}, true
			}
		}
	}
	return CorePackageMatch{}, false
}

func IsCorePackage(request PackageRequest) bool {
	_, ok := NormalizeCorePackage(request)
	return ok
}

func lowercasePackageName(name BarePackageName) BarePackageName {
	return BarePackageName(strings.ToLower(string(name)))
}
