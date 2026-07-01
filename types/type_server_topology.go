package types

import (
	"slices"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

type RuntimeNodeID string

const RuntimeNodeUnknown RuntimeNodeID = ""

const (
	RuntimeNodeMinecraft        RuntimeNodeID = "minecraft"
	RuntimeNodeFabric           RuntimeNodeID = "fabric"
	RuntimeNodeForge            RuntimeNodeID = "forge"
	RuntimeNodeNeoforge         RuntimeNodeID = "neoforge"
	RuntimeNodeMCDR             RuntimeNodeID = "mcdr"
	RuntimeNodePaper            RuntimeNodeID = "paper"
	RuntimeNodeSpigot           RuntimeNodeID = "spigot"
	RuntimeNodePaperFork        RuntimeNodeID = "paper-fork"
	RuntimeNodeCraftBukkit      RuntimeNodeID = "craftbukkit"
	RuntimeNodeBukkit           RuntimeNodeID = "bukkit"
	RuntimeNodeFolia            RuntimeNodeID = "folia"
	RuntimeNodeLeaves           RuntimeNodeID = "leaves"
	RuntimeNodeSponge           RuntimeNodeID = "sponge"
	RuntimeNodeArclight         RuntimeNodeID = "arclight"
	RuntimeNodeCatServer        RuntimeNodeID = "catserver"
	RuntimeNodeYouer            RuntimeNodeID = "youer"
	RuntimeNodeVelocity         RuntimeNodeID = "velocity"
	RuntimeNodeBungeecord       RuntimeNodeID = "bungeecord"
	RuntimeNodeWaterfall        RuntimeNodeID = "waterfall"
	RuntimeNodeGeyser           RuntimeNodeID = "geyser"
	RuntimeNodeGeyserStandalone RuntimeNodeID = "geyser_standalone"
	RuntimeNodeConnector        RuntimeNodeID = "connector"
	RuntimeNodeKilt             RuntimeNodeID = "kilt"
)

type RuntimeRole string

const (
	RuntimeRoleModLoader      RuntimeRole = "mod_loader"      // jvm-injecting mod loaders
	RuntimeRolePluginCore     RuntimeRole = "plugin_core"     // cores based on exposed NMS APIs, e.g. craftbukkit derivatives, velocity, sponge. MCDR is included here for now unless there's a strong reason to separate it.
	RuntimeRoleHybrid         RuntimeRole = "hybrid"          // complex runtimes
	RuntimeRoleProxy          RuntimeRole = "proxy"           // proxy servers that do not actually host a Minecraft runtime, e.g. velocity, bungeecord
	RuntimeRoleBridge         RuntimeRole = "bridge"          // bridge layers, e.g. sinytra connector and kilt
	RuntimeRoleProtocolBridge RuntimeRole = "protocol_bridge" // Java <-> Bedrock bridges, dedicated for geyser for now
	RuntimeRoleVanilla        RuntimeRole = "vanilla"         // self-explanatory
	RuntimeRoleUnknown        RuntimeRole = ""                // sentinel value
)

type RuntimeCapability string

const (
	CapabilityFabricLoader  RuntimeCapability = "fabric_mods"
	CapabilityForge         RuntimeCapability = "forge_mods"
	CapabilityNeoforge      RuntimeCapability = "neoforge_mods"
	CapabilityBukkitAPI     RuntimeCapability = "bukkit_plugins"
	CapabilitySpigotAPI     RuntimeCapability = "spigot_plugins"
	CapabilityPaperAPI      RuntimeCapability = "paper_plugins"
	CapabilityPurpurAPI     RuntimeCapability = "purpur_plugins"
	CapabilityFoliaAPI      RuntimeCapability = "folia_plugins"
	CapabilityVelocity      RuntimeCapability = "velocity_plugins"
	CapabilityBungeecord    RuntimeCapability = "bungeecord_plugins"
	CapabilityMcdr          RuntimeCapability = "mcdr_plugins"
	CapabilitySpongeAPI     RuntimeCapability = "sponge_plugins"
	CapabilityReversedProxy RuntimeCapability = "proxying"
	CapabilityBedrockBridge RuntimeCapability = "protocol_bridge"
)

func (c RuntimeCapability) String() string {
	return cases.Title(language.English).String(
		strings.ReplaceAll(string(c), "_", " "),
	)
}

func (c RuntimeCapability) Populate() []RuntimeCapability {
	switch c {
	case CapabilitySpigotAPI:
		return []RuntimeCapability{
			CapabilitySpigotAPI, CapabilityBukkitAPI,
		}
	case CapabilityPaperAPI:
		caps := []RuntimeCapability{CapabilityPaperAPI}
		caps = append(caps, CapabilitySpigotAPI.Populate()...)
		return caps
	case CapabilityPurpurAPI:
		caps := []RuntimeCapability{CapabilityPurpurAPI}
		caps = append(caps, CapabilityPaperAPI.Populate()...)
		return caps
	case CapabilityFoliaAPI:
		// folia compatibility is opt-in by a field in the plugin metadata.
		// however, we will still mark folia having lower rank bukkit
		// capabilities here. wether to enforce compatibility policy or not
		// is up to higher level consumers.
		caps := []RuntimeCapability{CapabilityFoliaAPI}
		caps = append(caps, CapabilityPaperAPI.Populate()...)
		return caps
	default:
		return []RuntimeCapability{c}
	}
}

type CompatVerdict string

const (
	CompatCompatible   CompatVerdict = "compatible"
	CompatDegraded     CompatVerdict = "degraded"
	CompatIncompatible CompatVerdict = "incompatible"
	CompatUnresolved   CompatVerdict = "unresolved"
)

// CompatResult reports only the compatibility verdict and its explanation.
type CompatResult struct {
	Verdict CompatVerdict `json:"verdict"`
	Reason  string        `json:"reason"`
	Detail  string        `json:"detail"`
}

// CompatPolicy describes the compatibility relationship between a server runtime
// and package ecosystem. All edges are directed: "can runtime A host packages for ecosystem B?"
type CompatPolicy struct {
	// HostNodeID is the runtime that hosts/runs the packages.
	HostNodeID RuntimeNodeID `json:"host_node_id"`
	// PackageEcosystem is the capability (ecosystem) the packages belong to.
	PackageEcosystem RuntimeCapability `json:"package_ecosystem"`
	// Verdict is the base verdict for this relationship (without bridge layers).
	Verdict CompatVerdict `json:"verdict"`
	// Reason is a machine-readable code for why this verdict was reached.
	Reason string `json:"reason"`
}

// RuntimeNode describes a materialized runtime layer. Identities holds the versioned
// package refs the node itself provides, so layers that bundle multiple runtime
// packages (e.g. connector with fabricloader) keep that association.
type RuntimeNode struct {
	ID           RuntimeNodeID         `json:"id"`
	Role         RuntimeRole           `json:"role"`
	Capabilities []RuntimeCapability   `json:"capabilities"`
	Identities   []VersionedPackageRef `json:"identities,omitempty"`
}

type TopologyNode = RuntimeNode

func (n RuntimeNode) HasCapability(c RuntimeCapability) bool {
	return slices.Contains(n.Capabilities, c)
}

// RuntimeEdgeVerb describes the type of relationship between two nodes in the topology.
type RuntimeEdgeVerb string

const (
	EdgeHosts   RuntimeEdgeVerb = "hosts"   // when a node hosts another node, e.g. a neoforge server hosting a sinytra layer
	EdgeExtends RuntimeEdgeVerb = "extends" // structural lineage: fork-of (purpur -> paper), anchor-to-vanilla (paper -> minecraft), etc. Compatibility and capabilities stay on nodes, not on this edge.
	EdgeProxies RuntimeEdgeVerb = "proxies" // multi-server modelling, e.g. velocity proxying to a paper backend
)

// RuntimeEdge records only structural relationships between runtime nodes.
type RuntimeEdge struct {
	From RuntimeNodeID   `json:"from"`
	To   RuntimeNodeID   `json:"to"`
	Verb RuntimeEdgeVerb `json:"verb"`
}

type ServerTopology struct {
	PrimaryNode RuntimeNodeID `json:"primary_node"`
	Nodes       []RuntimeNode `json:"nodes"`
	Edges       []RuntimeEdge `json:"edges"`
}

var (
	TopologyEmpty   = &ServerTopology{}
	TopologyUnknown = &ServerTopology{
		PrimaryNode: "unknown",
		Nodes:       []RuntimeNode{{ID: "unknown", Role: RuntimeRoleUnknown}},
		Edges:       nil,
	}
)

func (t *ServerTopology) Resolved() bool {
	return t != nil && t.PrimaryNode != RuntimeNodeUnknown && len(t.Nodes) > 0
}

func (t *ServerTopology) FindNode(id RuntimeNodeID) (RuntimeNode, bool) {
	if t == nil {
		return RuntimeNode{}, false
	}

	for _, node := range t.Nodes {
		if node.ID == id {
			return node, true
		}
	}

	return RuntimeNode{}, false
}

func (t *ServerTopology) HasCapability(c RuntimeCapability) bool {
	if t == nil {
		return false
	}

	for _, node := range t.Nodes {
		if node.HasCapability(c) {
			return true
		}
	}

	return false
}

func (t *ServerTopology) PrimaryNodeData() (RuntimeNode, bool) {
	if t == nil {
		return RuntimeNode{}, false
	}

	return t.FindNode(t.PrimaryNode)
}

// EdgesFrom returns all edges originating from a given node.
func (t *ServerTopology) EdgesFrom(id RuntimeNodeID) []RuntimeEdge {
	if t == nil {
		return []RuntimeEdge{}
	}

	edges := make([]RuntimeEdge, 0)
	for _, edge := range t.Edges {
		if edge.From == id {
			edges = append(edges, edge)
		}
	}

	return edges
}

// EdgesTo returns all edges pointing to a given node.
func (t *ServerTopology) EdgesTo(id RuntimeNodeID) []RuntimeEdge {
	if t == nil {
		return []RuntimeEdge{}
	}

	edges := make([]RuntimeEdge, 0)
	for _, edge := range t.Edges {
		if edge.To == id {
			edges = append(edges, edge)
		}
	}

	return edges
}

// NodesWithCapability returns all nodes that have the given capability.
func (t *ServerTopology) NodesWithCapability(c RuntimeCapability) []RuntimeNode {
	if t == nil {
		return []RuntimeNode{}
	}

	nodes := make([]RuntimeNode, 0)
	for _, node := range t.Nodes {
		if node.HasCapability(c) {
			nodes = append(nodes, node)
		}
	}

	return nodes
}

// PrimaryCapabilities returns the capabilities of the primary node only.
// Returns nil if topology is unresolved.
func (t *ServerTopology) PrimaryCapabilities() []RuntimeCapability {
	if t == nil {
		return []RuntimeCapability{}
	}

	if !t.Resolved() {
		return nil
	}

	primaryNode, ok := t.PrimaryNodeData()
	if !ok {
		return nil
	}

	return append([]RuntimeCapability(nil), primaryNode.Capabilities...)
}

// NodeIdentities returns the versioned package refs attached to the node with the
// given id. Returns an empty slice if the topology is nil or the node is absent.
func (t *ServerTopology) NodeIdentities(id RuntimeNodeID) []VersionedPackageRef {
	if t == nil {
		return []VersionedPackageRef{}
	}

	node, ok := t.FindNode(id)
	if !ok {
		return []VersionedPackageRef{}
	}

	return append([]VersionedPackageRef(nil), node.Identities...)
}

// AllIdentities collects versioned package refs from every node in the topology
// in node order. Returns an empty slice if the topology is nil.
func (t *ServerTopology) AllIdentities() []VersionedPackageRef {
	if t == nil {
		return []VersionedPackageRef{}
	}

	identities := make([]VersionedPackageRef, 0)
	for _, node := range t.Nodes {
		identities = append(identities, node.Identities...)
	}

	return identities
}
