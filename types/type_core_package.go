package types

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

type corePackageRoute struct {
	Scope SourceId
	Ref   PackageRef
}

type corePackageDefinition struct {
	Core   CorePackage
	Ref    PackageRef
	Routes []corePackageRoute
}

type corePackageRouteKey struct {
	Scope SourceId
	Eco   Ecosystem
	Name  BarePackageName
}

type corePackageCatalog struct {
	byRoute       map[corePackageRouteKey]CorePackageMatch
	canonical     map[PackageRef]CorePackage
	inferredAuto  map[PackageRef]CorePackageMatch
	ambiguousAuto map[PackageRef]bool
}

type corePackageCatalogError string

func (e corePackageCatalogError) Error() string {
	return string(e)
}

var corePackageDefinitions = []corePackageDefinition{
	{
		Core: CoreMinecraft,
		Ref:  PackageRef{Eco: EcoMinecraft, Name: "minecraft"},
		Routes: []corePackageRoute{
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoUnspecified, Name: "minecraft"}},
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoUnspecified, Name: "mc"}},
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoMinecraft, Name: "minecraft"}},
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoMinecraft, Name: "mc"}},
		},
	},
	{
		Core: CoreFabric,
		Ref:  PackageRef{Eco: EcoFabric, Name: "fabric"},
		Routes: []corePackageRoute{
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoUnspecified, Name: "fabric"}},
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoUnspecified, Name: "fabric-loader"}},
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoFabric, Name: "fabric"}},
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoFabric, Name: "fabric-loader"}},
		},
	},
	{
		Core: CoreForge,
		Ref:  PackageRef{Eco: EcoForge, Name: "forge"},
		Routes: []corePackageRoute{
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoUnspecified, Name: "forge"}},
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoForge, Name: "forge"}},
		},
	},
	{
		Core: CoreNeoForge,
		Ref:  PackageRef{Eco: EcoNeoforge, Name: "neoforge"},
		Routes: []corePackageRoute{
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoUnspecified, Name: "neoforge"}},
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoNeoforge, Name: "neoforge"}},
		},
	},
	{
		Core: CoreMCDReforged,
		Ref:  PackageRef{Eco: EcoMcdr, Name: "mcdreforged"},
		Routes: []corePackageRoute{
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoUnspecified, Name: "mcdreforged"}},
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoUnspecified, Name: "mcdr"}},
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoMcdr, Name: "mcdreforged"}},
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoMcdr, Name: "mcdr"}},
		},
	},
	{
		Core: CoreCraftBukkit,
		Ref:  PackageRef{Eco: EcoBukkit, Name: "craftbukkit"},
		Routes: []corePackageRoute{
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoUnspecified, Name: "bukkit"}},
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoUnspecified, Name: "craftbukkit"}},
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoBukkit, Name: "bukkit"}},
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoBukkit, Name: "craftbukkit"}},
		},
	},
	{
		Core: CoreSpigot,
		Ref:  PackageRef{Eco: EcoBukkit, Name: "spigot"},
		Routes: []corePackageRoute{
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoUnspecified, Name: "spigot"}},
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoBukkit, Name: "spigot"}},
		},
	},
	{
		Core: CorePaper,
		Ref:  PackageRef{Eco: EcoPaper, Name: "paper"},
		Routes: []corePackageRoute{
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoUnspecified, Name: "paper"}},
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoPaper, Name: "paper"}},
		},
	},
	{
		Core: CoreFolia,
		Ref:  PackageRef{Eco: EcoPaper, Name: "folia"},
		Routes: []corePackageRoute{
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoUnspecified, Name: "folia"}},
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoPaper, Name: "folia"}},
		},
	},
	{
		Core: CoreLeaves,
		Ref:  PackageRef{Eco: EcoPaper, Name: "leaves"},
		Routes: []corePackageRoute{
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoUnspecified, Name: "leaves"}},
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoPaper, Name: "leaves"}},
		},
	},
	{
		Core: CoreArclight,
		Ref:  PackageRef{Eco: EcoUnspecified, Name: "arclight"},
		Routes: []corePackageRoute{
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoUnspecified, Name: "arclight"}},
		},
	},
	{
		Core: CoreArclightForge,
		Ref:  PackageRef{Eco: EcoUnspecified, Name: "arclight-forge"},
		Routes: []corePackageRoute{
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoUnspecified, Name: "arclight-forge"}},
		},
	},
	{
		Core: CoreArclightNeoForge,
		Ref:  PackageRef{Eco: EcoUnspecified, Name: "arclight-neoforge"},
		Routes: []corePackageRoute{
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoUnspecified, Name: "arclight-neoforge"}},
		},
	},
	{
		Core: CoreArclightFabric,
		Ref:  PackageRef{Eco: EcoUnspecified, Name: "arclight-fabric"},
		Routes: []corePackageRoute{
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoUnspecified, Name: "arclight-fabric"}},
		},
	},
	{
		Core: CoreCatServer,
		Ref:  PackageRef{Eco: EcoUnspecified, Name: "catserver"},
		Routes: []corePackageRoute{
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoUnspecified, Name: "catserver"}},
		},
	},
	{
		Core: CoreYouer,
		Ref:  PackageRef{Eco: EcoUnspecified, Name: "youer"},
		Routes: []corePackageRoute{
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoUnspecified, Name: "youer"}},
		},
	},
	{
		Core: CoreSpongeVanilla,
		Ref:  PackageRef{Eco: EcoSponge, Name: "spongevanilla"},
		Routes: []corePackageRoute{
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoUnspecified, Name: "sponge"}},
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoUnspecified, Name: "spongevanilla"}},
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoSponge, Name: "sponge"}},
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoSponge, Name: "spongevanilla"}},
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoSponge, Name: "vanilla"}},
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoSponge, Name: "minecraft"}},
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoSponge, Name: "mc"}},
		},
	},
	{
		Core: CoreSpongeForge,
		Ref:  PackageRef{Eco: EcoSponge, Name: "spongeforge"},
		Routes: []corePackageRoute{
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoUnspecified, Name: "spongeforge"}},
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoSponge, Name: "spongeforge"}},
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoSponge, Name: "forge"}},
		},
	},
	{
		Core: CoreSpongeNeo,
		Ref:  PackageRef{Eco: EcoSponge, Name: "spongeneo"},
		Routes: []corePackageRoute{
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoUnspecified, Name: "spongeneo"}},
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoSponge, Name: "spongeneo"}},
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoSponge, Name: "neo"}},
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoSponge, Name: "neoforge"}},
		},
	},
	{
		Core: CoreBungeeCord,
		Ref:  PackageRef{Eco: EcoBungeecord, Name: "bungeecord"},
		Routes: []corePackageRoute{
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoUnspecified, Name: "bungeecord"}},
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoBungeecord, Name: "bungeecord"}},
		},
	},
	{
		Core: CoreVelocity,
		Ref:  PackageRef{Eco: EcoVelocity, Name: "velocity"},
		Routes: []corePackageRoute{
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoUnspecified, Name: "velocity"}},
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoVelocity, Name: "velocity"}},
		},
	},
	{
		Core: CoreWaterfall,
		Ref:  PackageRef{Eco: EcoBungeecord, Name: "waterfall"},
		Routes: []corePackageRoute{
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoUnspecified, Name: "waterfall"}},
			{Scope: SourceAuto, Ref: PackageRef{Eco: EcoBungeecord, Name: "waterfall"}},
		},
	},
}

var loadedCorePackageCatalog, loadedCorePackageCatalogErr = newCorePackageCatalog()

func NormalizeCorePackage(
	request ScopedPackageRef,
) (CorePackageMatch, bool, error) {
	if loadedCorePackageCatalogErr != nil {
		return CorePackageMatch{}, false, loadedCorePackageCatalogErr
	}

	request.Name = lowercasePackageName(request.Name)
	key := corePackageRouteKey{
		Scope: request.Scope,
		Eco:   request.Eco,
		Name:  request.Name,
	}
	if match, ok := loadedCorePackageCatalog.byRoute[key]; ok {
		match.Ref.Scope = request.Scope
		return match, true, nil
	}

	if request.Scope != SourceAuto {
		key.Scope = SourceAuto
		if match, ok := loadedCorePackageCatalog.byRoute[key]; ok {
			match.Ref.Scope = request.Scope
			return match, true, nil
		}
		return CorePackageMatch{}, false, nil
	}

	ref := PackageRef{Eco: request.Eco, Name: request.Name}
	if loadedCorePackageCatalog.ambiguousAuto[ref] {
		return CorePackageMatch{}, false, corePackageCatalogError(
			"core package catalog: ambiguous automatic route " + ref.StringBase(),
		)
	}
	match, ok := loadedCorePackageCatalog.inferredAuto[ref]
	if !ok {
		return CorePackageMatch{}, false, nil
	}
	match.Ref.Scope = SourceAuto
	return match, true, nil
}

func IsCorePackage(ref PackageRef) bool {
	if loadedCorePackageCatalogErr != nil {
		return false
	}
	_, ok := loadedCorePackageCatalog.canonical[ref]
	return ok
}

func newCorePackageCatalog() (*corePackageCatalog, error) {
	catalog := &corePackageCatalog{
		byRoute:       make(map[corePackageRouteKey]CorePackageMatch),
		canonical:     make(map[PackageRef]CorePackage),
		inferredAuto:  make(map[PackageRef]CorePackageMatch),
		ambiguousAuto: make(map[PackageRef]bool),
	}

	for _, definition := range corePackageDefinitions {
		if !validCanonicalCorePackageRef(definition.Core, definition.Ref) {
			return nil, corePackageCatalogError(
				"core package catalog: invalid canonical reference " + definition.Ref.StringBase(),
			)
		}
		if owner, ok := catalog.canonical[definition.Ref]; ok && owner != definition.Core {
			return nil, corePackageCatalogError(
				"core package catalog: canonical reference has multiple owners " + definition.Ref.StringBase(),
			)
		}
		catalog.canonical[definition.Ref] = definition.Core

		for _, route := range definition.Routes {
			if route.Scope == SourceUnknown || route.Ref.Name == "" {
				return nil, corePackageCatalogError("core package catalog: invalid request route")
			}
			route.Ref.Name = lowercasePackageName(route.Ref.Name)
			key := corePackageRouteKey{
				Scope: route.Scope,
				Eco:   route.Ref.Eco,
				Name:  route.Ref.Name,
			}
			if _, exists := catalog.byRoute[key]; exists {
				return nil, corePackageCatalogError(
					"core package catalog: duplicate request route " + route.Ref.StringBase(),
				)
			}

			match := CorePackageMatch{
				Core: definition.Core,
				Ref: ScopedPackageRef{
					PackageRef: definition.Ref,
					Scope:      route.Scope,
				},
			}
			catalog.byRoute[key] = match
			if route.Scope == SourceAuto {
				continue
			}

			inferred, exists := catalog.inferredAuto[route.Ref]
			if !exists {
				catalog.inferredAuto[route.Ref] = match
				continue
			}
			if inferred.Core != match.Core {
				catalog.ambiguousAuto[route.Ref] = true
			}
		}
	}

	return catalog, nil
}

func validCanonicalCorePackageRef(core CorePackage, ref PackageRef) bool {
	if ref.Name != BarePackageName(core) {
		return false
	}

	switch core {
	case CoreMinecraft:
		return ref.Eco == EcoMinecraft
	case CoreFabric:
		return ref.Eco == EcoFabric
	case CoreForge:
		return ref.Eco == EcoForge
	case CoreNeoForge:
		return ref.Eco == EcoNeoforge
	case CoreMCDReforged:
		return ref.Eco == EcoMcdr
	case CoreCraftBukkit, CoreSpigot:
		return ref.Eco == EcoBukkit
	case CorePaper, CoreFolia, CoreLeaves:
		return ref.Eco == EcoPaper
	case CoreArclight, CoreArclightForge, CoreArclightNeoForge,
		CoreArclightFabric, CoreCatServer, CoreYouer:
		return ref.Eco == EcoUnspecified
	case CoreSpongeVanilla, CoreSpongeForge, CoreSpongeNeo:
		return ref.Eco == EcoSponge
	case CoreBungeeCord, CoreWaterfall:
		return ref.Eco == EcoBungeecord
	case CoreVelocity:
		return ref.Eco == EcoVelocity
	default:
		return false
	}
}

func lowercasePackageName(name BarePackageName) BarePackageName {
	lower := make([]byte, len(name))
	for i := range name {
		b := name[i]
		if b >= 'A' && b <= 'Z' {
			b += 'a' - 'A'
		}
		lower[i] = b
	}
	return BarePackageName(lower)
}
