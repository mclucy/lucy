package types

import "strings"

// CorePackage identifies a special request for a bootable server product or
// platform installer. It classifies package requests only; installation policy
// belongs to install and bootstrap.
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
	Ref  ScopedPackageRef
}

type corePackageDefinition struct {
	Core    CorePackage
	Ref     PackageRef
	Aliases []PackageRef
}

// corePackageDefinitions maps user-facing spellings to canonical core
// identities. Ref is the canonical reference; Aliases are every accepted
// request spelling, including the canonical one. Static invariants (valid
// canonical eco, no duplicate aliases) are enforced by tests.
var corePackageDefinitions = []corePackageDefinition{
	{
		Core: CoreMinecraft,
		Ref:  PackageRef{Eco: EcoMinecraft, Name: "minecraft"},
		Aliases: []PackageRef{
			{Eco: EcoUnspecified, Name: "minecraft"},
			{Eco: EcoUnspecified, Name: "mc"},
			{Eco: EcoMinecraft, Name: "minecraft"},
			{Eco: EcoMinecraft, Name: "mc"},
		},
	},
	{
		Core: CoreFabric,
		Ref:  PackageRef{Eco: EcoFabric, Name: "fabric"},
		Aliases: []PackageRef{
			{Eco: EcoUnspecified, Name: "fabric"},
			{Eco: EcoUnspecified, Name: "fabric-loader"},
			{Eco: EcoFabric, Name: "fabric"},
			{Eco: EcoFabric, Name: "fabric-loader"},
		},
	},
	{
		Core: CoreForge,
		Ref:  PackageRef{Eco: EcoForge, Name: "forge"},
		Aliases: []PackageRef{
			{Eco: EcoUnspecified, Name: "forge"},
			{Eco: EcoForge, Name: "forge"},
		},
	},
	{
		Core: CoreNeoForge,
		Ref:  PackageRef{Eco: EcoNeoforge, Name: "neoforge"},
		Aliases: []PackageRef{
			{Eco: EcoUnspecified, Name: "neoforge"},
			{Eco: EcoNeoforge, Name: "neoforge"},
		},
	},
	{
		Core: CoreMCDReforged,
		Ref:  PackageRef{Eco: EcoMcdr, Name: "mcdreforged"},
		Aliases: []PackageRef{
			{Eco: EcoUnspecified, Name: "mcdreforged"},
			{Eco: EcoUnspecified, Name: "mcdr"},
			{Eco: EcoMcdr, Name: "mcdreforged"},
			{Eco: EcoMcdr, Name: "mcdr"},
		},
	},
	{
		Core: CoreCraftBukkit,
		Ref:  PackageRef{Eco: EcoBukkit, Name: "craftbukkit"},
		Aliases: []PackageRef{
			{Eco: EcoUnspecified, Name: "bukkit"},
			{Eco: EcoUnspecified, Name: "craftbukkit"},
			{Eco: EcoBukkit, Name: "bukkit"},
			{Eco: EcoBukkit, Name: "craftbukkit"},
		},
	},
	{
		Core: CoreSpigot,
		Ref:  PackageRef{Eco: EcoBukkit, Name: "spigot"},
		Aliases: []PackageRef{
			{Eco: EcoUnspecified, Name: "spigot"},
			{Eco: EcoBukkit, Name: "spigot"},
		},
	},
	{
		Core: CorePaper,
		Ref:  PackageRef{Eco: EcoPaper, Name: "paper"},
		Aliases: []PackageRef{
			{Eco: EcoUnspecified, Name: "paper"},
			{Eco: EcoPaper, Name: "paper"},
		},
	},
	{
		Core: CoreFolia,
		Ref:  PackageRef{Eco: EcoPaper, Name: "folia"},
		Aliases: []PackageRef{
			{Eco: EcoUnspecified, Name: "folia"},
			{Eco: EcoPaper, Name: "folia"},
		},
	},
	{
		Core: CoreLeaves,
		Ref:  PackageRef{Eco: EcoPaper, Name: "leaves"},
		Aliases: []PackageRef{
			{Eco: EcoUnspecified, Name: "leaves"},
			{Eco: EcoPaper, Name: "leaves"},
		},
	},
	{
		Core: CoreArclight,
		Ref:  PackageRef{Eco: EcoUnspecified, Name: "arclight"},
		Aliases: []PackageRef{
			{Eco: EcoUnspecified, Name: "arclight"},
		},
	},
	{
		Core: CoreArclightForge,
		Ref:  PackageRef{Eco: EcoUnspecified, Name: "arclight-forge"},
		Aliases: []PackageRef{
			{Eco: EcoUnspecified, Name: "arclight-forge"},
		},
	},
	{
		Core: CoreArclightNeoForge,
		Ref:  PackageRef{Eco: EcoUnspecified, Name: "arclight-neoforge"},
		Aliases: []PackageRef{
			{Eco: EcoUnspecified, Name: "arclight-neoforge"},
		},
	},
	{
		Core: CoreArclightFabric,
		Ref:  PackageRef{Eco: EcoUnspecified, Name: "arclight-fabric"},
		Aliases: []PackageRef{
			{Eco: EcoUnspecified, Name: "arclight-fabric"},
		},
	},
	{
		Core: CoreCatServer,
		Ref:  PackageRef{Eco: EcoUnspecified, Name: "catserver"},
		Aliases: []PackageRef{
			{Eco: EcoUnspecified, Name: "catserver"},
		},
	},
	{
		Core: CoreYouer,
		Ref:  PackageRef{Eco: EcoUnspecified, Name: "youer"},
		Aliases: []PackageRef{
			{Eco: EcoUnspecified, Name: "youer"},
		},
	},
	{
		Core: CoreSpongeVanilla,
		Ref:  PackageRef{Eco: EcoSponge, Name: "spongevanilla"},
		Aliases: []PackageRef{
			{Eco: EcoUnspecified, Name: "sponge"},
			{Eco: EcoUnspecified, Name: "spongevanilla"},
			{Eco: EcoSponge, Name: "sponge"},
			{Eco: EcoSponge, Name: "spongevanilla"},
			{Eco: EcoSponge, Name: "vanilla"},
			{Eco: EcoSponge, Name: "minecraft"},
			{Eco: EcoSponge, Name: "mc"},
		},
	},
	{
		Core: CoreSpongeForge,
		Ref:  PackageRef{Eco: EcoSponge, Name: "spongeforge"},
		Aliases: []PackageRef{
			{Eco: EcoUnspecified, Name: "spongeforge"},
			{Eco: EcoSponge, Name: "spongeforge"},
			{Eco: EcoSponge, Name: "forge"},
		},
	},
	{
		Core: CoreSpongeNeo,
		Ref:  PackageRef{Eco: EcoSponge, Name: "spongeneo"},
		Aliases: []PackageRef{
			{Eco: EcoUnspecified, Name: "spongeneo"},
			{Eco: EcoSponge, Name: "spongeneo"},
			{Eco: EcoSponge, Name: "neo"},
			{Eco: EcoSponge, Name: "neoforge"},
		},
	},
	{
		Core: CoreBungeeCord,
		Ref:  PackageRef{Eco: EcoBungeecord, Name: "bungeecord"},
		Aliases: []PackageRef{
			{Eco: EcoUnspecified, Name: "bungeecord"},
			{Eco: EcoBungeecord, Name: "bungeecord"},
		},
	},
	{
		Core: CoreVelocity,
		Ref:  PackageRef{Eco: EcoVelocity, Name: "velocity"},
		Aliases: []PackageRef{
			{Eco: EcoUnspecified, Name: "velocity"},
			{Eco: EcoVelocity, Name: "velocity"},
		},
	},
	{
		Core: CoreWaterfall,
		Ref:  PackageRef{Eco: EcoBungeecord, Name: "waterfall"},
		Aliases: []PackageRef{
			{Eco: EcoUnspecified, Name: "waterfall"},
			{Eco: EcoBungeecord, Name: "waterfall"},
		},
	},
}

type corePackageEntry struct {
	Core CorePackage
	Ref  PackageRef
}

var corePackageByAlias, corePackageCanonical = buildCorePackageIndex()

func buildCorePackageIndex() (
	map[PackageRef]corePackageEntry,
	map[PackageRef]CorePackage,
) {
	byAlias := make(map[PackageRef]corePackageEntry)
	canonical := make(map[PackageRef]CorePackage)
	for _, definition := range corePackageDefinitions {
		canonical[definition.Ref] = definition.Core
		for _, alias := range definition.Aliases {
			alias.Name = lowercasePackageName(alias.Name)
			byAlias[alias] = corePackageEntry{
				Core: definition.Core,
				Ref:  definition.Ref,
			}
		}
	}
	return byAlias, canonical
}

// NormalizeCorePackage resolves a package request against the core catalog.
// Aliases are matched case-insensitively on any source scope; the returned
// match carries the canonical reference with the request's scope preserved.
func NormalizeCorePackage(request ScopedPackageRef) (CorePackageMatch, bool) {
	entry, ok := corePackageByAlias[PackageRef{
		Eco:  request.Eco,
		Name: lowercasePackageName(request.Name),
	}]
	if !ok {
		return CorePackageMatch{}, false
	}
	return CorePackageMatch{
		Core: entry.Core,
		Ref: ScopedPackageRef{
			PackageRef: entry.Ref,
			Scope:      request.Scope,
		},
	}, true
}

func IsCorePackage(ref PackageRef) bool {
	_, ok := corePackageCanonical[ref]
	return ok
}

func lowercasePackageName(name BarePackageName) BarePackageName {
	return BarePackageName(strings.ToLower(string(name)))
}
