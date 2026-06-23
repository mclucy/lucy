package types

// BarePackageName is an untrusted package name. Usually from user input. It might
// be invalid.
type BarePackageName string

type PackageRef struct {
	Platform PlatformId
	Name     BarePackageName
}

func (p PackageRef) StringFull() string {
	return p.StringBase()
}

func (p PackageRef) StringBase() string {
	return p.Platform.String() + "/" + p.Name.String()
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

// Core identifies a kind of Minecraft server core: a bootable runtime artifact
// that can be installed as the primary executable of a server directory. Core is
// an injective subset of RuntimeNodeID — every Core maps to exactly one node,
// but not every node is a Core (bridges and plugin-form protocol translators are
// not cores). The uint8 representation keeps Core type-distinct from the
// string-backed RuntimeNodeID so the compiler enforces the subset relationship
// at every use site.
type Core uint8

const (
	// CoreInvalid is the zero value of Core. It represents an uninitialized or
	// unknown core and must never be installed. Reserving zero prevents silent
	// treatment of uninitialized fields as a real core.
	CoreInvalid Core = iota

	// Cores whose node has RuntimeRoleVanilla or RuntimeRoleModLoader.
	CoreVanilla
	CoreFabricLoader
	CoreForge
	CoreNeoforge

	// Cores whose node has RuntimeRolePluginCore, ordered by Bukkit-family
	// ancestry rank (lowest rung first). The ordering lets callers compare
	// Cores within the Bukkit family to determine plugin-API compatibility.
	CoreBukkit
	CoreCraftBukkit
	CoreSpigot
	CorePaper
	CorePaperFork
	CoreFolia
	CoreLeaves
	CoreSponge

	// Cores whose node has RuntimeRoleHybrid.
	CoreArclight
	CoreCatServer
	CoreYouer

	// Cores whose node has RuntimeRoleProxy. Proxies are cores because they
	// are standalone JVM artifacts that boot on their own; this is required
	// for future multi-server modelling where a proxy fronts backend servers.
	CoreVelocity
	CoreBungeecord
	CoreWaterfall
	CoreGeyserStandalone

	// CoreMCDR runs the server as a subprocess via stdin/stdout orchestration.
	// It is modelled as a core because it boots as a standalone JVM, even
	// though architecturally it is an external orchestrator rather than a
	// mod loader in the traditional sense.
	CoreMCDR
)

// CoreToNodeId is the single injective mapping from Core to RuntimeNodeID.
// Every Core has exactly one corresponding node; nodes with no Core
// representation (geyser plugin form, connector, kilt) return RuntimeNodeUnknown
// from the inverse direction.
func CoreToNodeId(c Core) RuntimeNodeID {
	switch c {
	case CoreVanilla:
		return RuntimeNodeMinecraft
	case CoreFabricLoader:
		return RuntimeNodeFabric
	case CoreForge:
		return RuntimeNodeForge
	case CoreNeoforge:
		return RuntimeNodeNeoforge
	case CoreBukkit:
		return RuntimeNodeBukkit
	case CoreCraftBukkit:
		return RuntimeNodeCraftBukkit
	case CoreSpigot:
		return RuntimeNodeSpigot
	case CorePaper:
		return RuntimeNodePaper
	case CorePaperFork:
		return RuntimeNodePaperFork
	case CoreFolia:
		return RuntimeNodeFolia
	case CoreLeaves:
		return RuntimeNodeLeaves
	case CoreSponge:
		return RuntimeNodeSponge
	case CoreArclight:
		return RuntimeNodeArclight
	case CoreCatServer:
		return RuntimeNodeCatServer
	case CoreYouer:
		return RuntimeNodeYouer
	case CoreVelocity:
		return RuntimeNodeVelocity
	case CoreBungeecord:
		return RuntimeNodeBungeecord
	case CoreWaterfall:
		return RuntimeNodeWaterfall
	case CoreGeyserStandalone:
		return RuntimeNodeGeyserStandalone
	case CoreMCDR:
		return RuntimeNodeMCDR
	default:
		return RuntimeNodeUnknown
	}
}

// String returns the canonical lowercase identifier of the core, matching the
// RuntimeNodeID of its corresponding node. This is the form used in serialized
// state, CLI display, and user input.
func (core Core) String() string {
	return string(CoreToNodeId(core))
}
