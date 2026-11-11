// Package catalog describes package identities, artifacts, compatibility,
// metadata, and dependencies independently of their source.
//
// Runtime observations, persisted workspace state, provider clients, and
// presentation belong to their owning packages, not catalog.
package catalog

import (
	"slices"

	"github.com/mclucy/lucy/version"
)

// Ecosystem identifies the compatibility surface a server can serve. It is a
// capability descriptor, not the server product itself.
type Ecosystem string

const (
	EcosystemUnspecified Ecosystem = ""

	EcosystemMinecraft Ecosystem = "minecraft"
	EcosystemVanilla             = EcosystemMinecraft

	EcosystemFabric   Ecosystem = "fabric"
	EcosystemQuilt    Ecosystem = "quilt"
	EcosystemForge    Ecosystem = "forge"
	EcosystemNeoForge Ecosystem = "neoforge"

	EcosystemBukkit     Ecosystem = "bukkit"
	EcosystemPaper      Ecosystem = "paper"
	EcosystemBungeeCord Ecosystem = "bungeecord"
	EcosystemVelocity   Ecosystem = "velocity"
	EcosystemSponge     Ecosystem = "sponge"
	EcosystemMCDR       Ecosystem = "mcdr"
)

// Family groups ecosystems whose packages deploy to the same kind of runtime.
// One deployable artifact never spans two families.
type Family uint8

const (
	FamilyNone Family = iota
	// FamilyMod is a mod loader that runs inside the game server.
	FamilyMod
	// FamilyPlugin is a plugin API that a game server implements.
	FamilyPlugin
	// FamilyProxy is a plugin API that a network proxy implements.
	FamilyProxy
)

type ecosystemDefinition struct {
	title      string
	searchable bool
	family     Family
	// dialect is the version grammar of the ecosystem's descriptors.
	dialect version.Dialect
	// alsoLoads lists the other ecosystems whose packages a runtime of this
	// ecosystem loads, in the order its loader prefers their descriptors
	// after its own.
	alsoLoads []Ecosystem
}

// ecosystemDefinitions is the single source of ecosystem traits. An
// ecosystem missing from it is not valid.
var ecosystemDefinitions = map[Ecosystem]ecosystemDefinition{
	EcosystemUnspecified: {title: "Any"},
	EcosystemMinecraft:   {title: "Minecraft"},
	EcosystemFabric: {
		title:      "Fabric",
		searchable: true,
		family:     FamilyMod,
		dialect:    version.DialectFabric,
	},
	EcosystemQuilt: {
		title:      "Quilt",
		searchable: true,
		family:     FamilyMod,
		dialect:    version.DialectFabric,
		alsoLoads:  []Ecosystem{EcosystemFabric},
	},
	EcosystemForge: {
		title:      "Forge",
		searchable: true,
		family:     FamilyMod,
		dialect:    version.DialectMaven,
	},
	EcosystemNeoForge: {
		title:      "NeoForge",
		searchable: true,
		family:     FamilyMod,
		dialect:    version.DialectMaven,
	},
	EcosystemBukkit: {
		title:      "Bukkit",
		searchable: true,
		family:     FamilyPlugin,
		dialect:    version.DialectOpaque,
	},
	EcosystemPaper: {
		title:      "Paper",
		searchable: true,
		family:     FamilyPlugin,
		dialect:    version.DialectOpaque,
		alsoLoads:  []Ecosystem{EcosystemBukkit},
	},
	EcosystemBungeeCord: {title: "BungeeCord", family: FamilyProxy},
	EcosystemVelocity:   {title: "Velocity", family: FamilyProxy},
	EcosystemSponge:     {title: "Sponge"},
	EcosystemMCDR:       {title: "MCDR", dialect: version.DialectMCDR},
}

func (e Ecosystem) Title() string {
	if definition, ok := ecosystemDefinitions[e]; ok {
		return definition.title
	}
	return "Unknown"
}

func (e Ecosystem) String() string {
	if e == EcosystemUnspecified {
		return "any"
	}
	return string(e)
}

// Valid reports whether e is a defined ecosystem.
func (e Ecosystem) Valid() bool {
	_, ok := ecosystemDefinitions[e]
	return ok
}

func (e Ecosystem) IsSearchable() bool {
	return ecosystemDefinitions[e].searchable
}

func (e Ecosystem) Family() Family {
	return ecosystemDefinitions[e].family
}

func (e Ecosystem) IsModding() bool {
	return e.Family() == FamilyMod
}

// VersionDialect returns the version grammar of e's descriptors, or
// version.DialectUnknown when e has no descriptor grammar.
func (e Ecosystem) VersionDialect() version.Dialect {
	return ecosystemDefinitions[e].dialect
}

// Loads lists the ecosystems whose packages a runtime of e loads, e first.
// The order is the loader's descriptor precedence: when one artifact carries
// descriptors of several listed ecosystems, the loader reads the earliest.
func (e Ecosystem) Loads() []Ecosystem {
	if e == EcosystemUnspecified {
		return nil
	}
	return append([]Ecosystem{e}, ecosystemDefinitions[e].alsoLoads...)
}

// Satisfy reports whether a runtime of e loads packages of the required
// ecosystem. An unspecified requirement accepts any ecosystem.
func (e Ecosystem) Satisfy(required Ecosystem) bool {
	if required == EcosystemUnspecified {
		return true
	}
	return slices.Contains(e.Loads(), required)
}

// ModuleNamespace returns the ecosystem that scopes module identities of
// descriptor ecosystem d in a runtime of e. A loader that loads several
// ecosystems resolves all their modules in one graph, so each of them maps to
// e. Any other ecosystem keeps its own namespace.
func (e Ecosystem) ModuleNamespace(d Ecosystem) Ecosystem {
	if e != EcosystemUnspecified && slices.Contains(e.Loads(), d) {
		return e
	}
	return d
}

// LoadedAlongside lists e and every ecosystem that some runtime loads
// together with e. Their modules can share one namespace, so a dependency
// recorded under one of them can name a package recorded under another.
func (e Ecosystem) LoadedAlongside() []Ecosystem {
	peers := []Ecosystem{e}
	for runtime := range ecosystemDefinitions {
		loads := runtime.Loads()
		if !slices.Contains(loads, e) {
			continue
		}
		for _, peer := range loads {
			if !slices.Contains(peers, peer) {
				peers = append(peers, peer)
			}
		}
	}
	slices.Sort(peers[1:])
	return peers
}

// IsUnspecified reports whether the ecosystem is waiting for workspace
// context.
func (e Ecosystem) IsUnspecified() bool {
	return e == EcosystemUnspecified
}
