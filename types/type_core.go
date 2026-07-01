package types

// BarePackageName is an untrusted package name. Usually from user input. It might
// be invalid.
type BarePackageName string

type PackageRef struct {
	Eco  Ecosystem
	Name BarePackageName
}

func (p PackageRef) StringFull() string {
	return p.StringBase()
}

func (p PackageRef) StringBase() string {
	return p.Eco.String() + "/" + p.Name.String()
}

type VersionedPackageRef struct {
	PackageRef
	Version BareVersion
}

type ScopedPackageRef struct {
	PackageRef
	Scope SourceId
}

func (p ScopedPackageRef) StringBase() string {
	return p.PackageRef.StringBase()
}

func (p ScopedPackageRef) StringFull() string {
	return p.Scope.String() + ":" + p.PackageRef.StringFull()
}

type FullPackageRef struct {
	PackageRef
	Version BareVersion
	Scope   SourceId
}

func (p FullPackageRef) StringBase() string {
	return p.PackageRef.StringBase()
}

func (p FullPackageRef) StringFull() string {
	return p.Scope.String() + ":" + p.PackageRef.StringFull()
}

type StringablePackageRef interface {
	StringFull() string
	StringBase() string
}

// Core is the registry of bootable Minecraft server cores: installable runtime
// artifacts that can serve as the primary executable of a server directory. Each
// key is the canonical identity package ref for that core; each value is the
// canonical lowercase name (RuntimeNodeID) used in topology, CLI, and state.
//
// Cores are an injective subset of runtime nodes — every entry maps to exactly
// one node, but not every node is a core (bridges and plugin-form protocol
// translators are omitted).
type Core string

const (
	CoreMinecraft Core = "minecraft"

	CoreFabric   Core = "fabric"
	CoreForge    Core = "forge"
	CoreNeoforge Core = "neoforge"

	CoreMcdr             Core = "mcdr"
	CoreCraftBukkit      Core = "craftbukkit"
	CoreSpigot           Core = "spigot"
	CorePaper            Core = "paper"
	CoreFolia            Core = "folia"
	CoreLeaves           Core = "leaves"
	CoreArclightForge    Core = "arclight-forge"
	CoreArclightNeoforge Core = "arclight-neoforge"
	CoreArclightFabric   Core = "arclight-fabric"
	CoreCatserver        Core = "catserver"
	CoreYouer            Core = "youer"

	CoreSpongeVanilla Core = "spongevanilla"
	CoreSpongeForge   Core = "spongeforge"
	CoreSpongeNeo     Core = "spongeneo"

	CoreBungeecord Core = "bungeecord"
	CoreVelocity   Core = "velocity"
	CoreWaterfall  Core = "waterfall"
)

// Cores is the authoritative core registry.
var Cores = map[PackageRef]Core{
	{Eco: EcoMinecraft, Name: "minecraft"}:   CoreMinecraft,
	{Eco: EcoUnspecified, Name: "minecraft"}: CoreMinecraft,
	{Eco: EcoUnspecified, Name: "mc"}:        CoreMinecraft,
	{Eco: EcoMinecraft, Name: "mc"}:          CoreMinecraft,

	{Eco: EcoFabric, Name: "fabric"}:             CoreFabric,
	{Eco: EcoFabric, Name: "fabric-loader"}:      CoreFabric,
	{Eco: EcoUnspecified, Name: "fabric"}:        CoreFabric,
	{Eco: EcoUnspecified, Name: "fabric-loader"}: CoreFabric,
	{Eco: EcoForge, Name: "forge"}:               CoreForge,
	{Eco: EcoUnspecified, Name: "forge"}:         CoreForge,
	{Eco: EcoNeoforge, Name: "neoforge"}:         CoreNeoforge,
	{Eco: EcoUnspecified, Name: "neoforge"}:      CoreNeoforge,

	{Eco: EcoMcdr, Name: "mcdreforged"}: CoreMcdr,

	{Eco: EcoUnspecified, Name: "bukkit"}:            CoreCraftBukkit,
	{Eco: EcoUnspecified, Name: "craftbukkit"}:       CoreCraftBukkit,
	{Eco: EcoUnspecified, Name: "spigot"}:            CoreSpigot,
	{Eco: EcoPaper, Name: "paper"}:                   CorePaper,
	{Eco: EcoUnspecified, Name: "paper"}:             CorePaper,
	{Eco: EcoUnspecified, Name: "folia"}:             CoreFolia,
	{Eco: EcoUnspecified, Name: "leaves"}:            CoreLeaves,
	{Eco: EcoUnspecified, Name: "arclight"}:          CoreArclightNeoforge,
	{Eco: EcoUnspecified, Name: "arclight-forge"}:    CoreArclightForge,
	{Eco: EcoUnspecified, Name: "arclight-neoforge"}: CoreArclightNeoforge,
	{Eco: EcoUnspecified, Name: "arclight-fabric"}:   CoreArclightFabric,
	{Eco: EcoUnspecified, Name: "catserver"}:         CoreCatserver,
	{Eco: EcoUnspecified, Name: "youer"}:             CoreYouer,
	{Eco: EcoVelocity, Name: "velocity"}:             CoreVelocity,

	{Eco: EcoSponge, Name: "spongevanilla"}:      CoreSpongeVanilla,
	{Eco: EcoUnspecified, Name: "spongevanilla"}: CoreSpongeVanilla,
	{Eco: EcoSponge, Name: "sponge"}:             CoreSpongeVanilla,
	{Eco: EcoSponge, Name: "vanilla"}:            CoreSpongeVanilla,
	{Eco: EcoSponge, Name: "minecraft"}:          CoreSpongeVanilla,
	{Eco: EcoSponge, Name: "mc"}:                 CoreSpongeVanilla,
	{Eco: EcoSponge, Name: "spongeforge"}:        CoreSpongeForge,
	{Eco: EcoUnspecified, Name: "spongeforge"}:   CoreSpongeForge,
	{Eco: EcoSponge, Name: "forge"}:              CoreSpongeForge,
	{Eco: EcoUnspecified, Name: "spongeneo"}:     CoreSpongeNeo,
	{Eco: EcoSponge, Name: "spongeneo"}:          CoreSpongeNeo,
	{Eco: EcoSponge, Name: "neo"}:                CoreSpongeNeo,
	{Eco: EcoSponge, Name: "neoforge"}:           CoreSpongeNeo,

	{Eco: EcoBungeecord, Name: "bungeecord"}:  CoreBungeecord,
	{Eco: EcoUnspecified, Name: "bungeecord"}: CoreBungeecord,
	{Eco: EcoVelocity, Name: "velocity"}:      CoreVelocity,
	{Eco: EcoUnspecified, Name: "velocity"}:   CoreVelocity,
	{Eco: EcoUnspecified, Name: "bungeecord"}: CoreVelocity,
	{Eco: EcoUnspecified, Name: "waterfall"}:  CoreWaterfall,
}

func LookupCore(ref PackageRef) (Core, bool) {
	if core, ok := Cores[ref]; ok {
		return core, true
	}
	unspecified := PackageRef{Eco: EcoUnspecified, Name: ref.Name}
	if core, ok := Cores[unspecified]; ok {
		return core, true
	}
	return "", false
}

func (c Core) SupportedEcosystems() []Ecosystem {
	switch c {
	case CoreFabric:
		return []Ecosystem{EcoFabric}
	case CoreForge:
		return []Ecosystem{EcoForge}
	case CoreNeoforge:
		return []Ecosystem{EcoNeoforge}
	case CoreMcdr:
		return []Ecosystem{EcoMcdr}
	case CoreCraftBukkit, CoreSpigot:
		return []Ecosystem{EcoBukkit}
	case CorePaper, CoreFolia, CoreLeaves:
		return []Ecosystem{EcoPaper, EcoBukkit}
	case CoreCatserver, CoreArclightForge:
		return []Ecosystem{EcoForge, EcoBukkit}
	case CoreArclightNeoforge:
		return []Ecosystem{EcoNeoforge, EcoBukkit}
	case CoreArclightFabric:
		return []Ecosystem{EcoFabric, EcoBukkit}
	case CoreYouer:
		return []Ecosystem{EcoNeoforge, EcoPaper, EcoBukkit}
	case CoreSpongeVanilla:
		return []Ecosystem{EcoSponge}
	case CoreSpongeForge:
		return []Ecosystem{EcoSponge, EcoForge}
	case CoreSpongeNeo:
		return []Ecosystem{EcoSponge, EcoNeoforge}
	case CoreBungeecord, CoreWaterfall:
		return []Ecosystem{EcoBungeecord}
	case CoreVelocity:
		return []Ecosystem{EcoVelocity}
	default:
		return nil
	}
}
