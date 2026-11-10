// Package catalog describes package identities, artifacts, compatibility,
// metadata, and dependencies independently of their source.
//
// Runtime observations, persisted workspace state, provider clients, and
// presentation belong to their owning packages, not catalog.
package catalog

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

func (e Ecosystem) Title() string {
	switch e {
	case EcosystemUnspecified:
		return "Any"
	case EcosystemMinecraft:
		return "Minecraft"
	case EcosystemFabric:
		return "Fabric"
	case EcosystemQuilt:
		return "Quilt"
	case EcosystemForge:
		return "Forge"
	case EcosystemNeoForge:
		return "NeoForge"
	case EcosystemBukkit:
		return "Bukkit"
	case EcosystemPaper:
		return "Paper"
	case EcosystemBungeeCord:
		return "BungeeCord"
	case EcosystemVelocity:
		return "Velocity"
	case EcosystemSponge:
		return "Sponge"
	case EcosystemMCDR:
		return "MCDR"
	default:
		return "Unknown"
	}
}

func (e Ecosystem) String() string {
	if e == EcosystemUnspecified {
		return "any"
	}
	return string(e)
}

// Valid reports whether e is a defined ecosystem.
func (e Ecosystem) Valid() bool {
	switch e {
	case EcosystemUnspecified,
		EcosystemMinecraft,
		EcosystemFabric,
		EcosystemQuilt,
		EcosystemForge,
		EcosystemNeoForge,
		EcosystemBukkit,
		EcosystemPaper,
		EcosystemBungeeCord,
		EcosystemVelocity,
		EcosystemSponge,
		EcosystemMCDR:
		return true
	default:
		return false
	}
}

func (e Ecosystem) IsSearchable() bool {
	switch e {
	case EcosystemFabric, EcosystemQuilt, EcosystemForge, EcosystemNeoForge,
		EcosystemBukkit, EcosystemPaper:
		return true
	default:
		return false
	}
}

// Satisfy reports whether e satisfies the required ecosystem. An unspecified
// requirement accepts any ecosystem. Supersets satisfy their base ecosystem:
// Paper satisfies Bukkit, and Quilt satisfies Fabric because Quilt Loader loads
// Fabric mods. Neither is reversible.
func (e Ecosystem) Satisfy(required Ecosystem) bool {
	if required == EcosystemUnspecified {
		return true
	}
	if e == EcosystemUnspecified {
		return false
	}
	if required == EcosystemBukkit && e == EcosystemPaper {
		return true
	}
	if required == EcosystemFabric && e == EcosystemQuilt {
		return true
	}
	return e == required
}

func (e Ecosystem) IsModding() bool {
	return e == EcosystemFabric || e == EcosystemQuilt ||
		e == EcosystemForge || e == EcosystemNeoForge
}

// IsUnspecified reports whether the ecosystem is waiting for workspace
// context.
func (e Ecosystem) IsUnspecified() bool {
	return e == EcosystemUnspecified
}
