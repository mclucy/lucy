package types

type RuntimeNodeID string

const RuntimeNodeUnknown RuntimeNodeID = ""

type RuntimeRole string

const (
	RuntimeRoleModLoader      RuntimeRole = "mod_loader"
	RuntimeRolePluginCore     RuntimeRole = "plugin_core"
	RuntimeRoleHybrid         RuntimeRole = "hybrid"
	RuntimeRoleProxy          RuntimeRole = "proxy"
	RuntimeRoleBridge         RuntimeRole = "bridge"
	RuntimeRoleProtocolBridge RuntimeRole = "protocol_bridge"
	RuntimeRoleVanilla        RuntimeRole = "vanilla"
	RuntimeRoleUnknown        RuntimeRole = ""
)

type RuntimeCapability string

const (
	CapabilityFabricMods     RuntimeCapability = "fabric_mods"
	CapabilityForgeMods      RuntimeCapability = "forge_mods"
	CapabilityNeoforgeMods   RuntimeCapability = "neoforge_mods"
	CapabilityBukkitPlugins  RuntimeCapability = "bukkit_plugins"
	CapabilityMCDRPlugins    RuntimeCapability = "mcdr_plugins"
	CapabilityProxying       RuntimeCapability = "proxying"
	CapabilityProtocolBridge RuntimeCapability = "protocol_bridge"
)

type RuntimeRiskLevel int

const (
	RiskNone     RuntimeRiskLevel = 0
	RiskLow      RuntimeRiskLevel = 1
	RiskMedium   RuntimeRiskLevel = 2
	RiskHigh     RuntimeRiskLevel = 3
	RiskCritical RuntimeRiskLevel = 4
)

type CompatVerdict string

const (
	CompatCompatible   CompatVerdict = "compatible"
	CompatDegraded     CompatVerdict = "degraded"
	CompatIncompatible CompatVerdict = "incompatible"
	CompatUnresolved   CompatVerdict = "unresolved"
)

type CompatResult struct {
	Verdict   CompatVerdict
	Reason    string
	Detail    string
	RiskLevel RuntimeRiskLevel
}

// CompatPolicy describes the compatibility relationship between a server runtime
// and package ecosystem. All edges are directed: "can runtime A host packages for ecosystem B?"
type CompatPolicy struct {
	// HostNodeID is the runtime that hosts/runs the packages.
	HostNodeID RuntimeNodeID
	// PackageEcosystem is the capability (ecosystem) the packages belong to.
	PackageEcosystem RuntimeCapability
	// Verdict is the base verdict for this relationship (without bridge layers).
	Verdict CompatVerdict
	// Risk is the risk level for this combination.
	Risk RuntimeRiskLevel
	// Reason is a machine-readable code for why this verdict was reached.
	Reason string
}

type RuntimeNode struct {
	ID               RuntimeNodeID
	Role             RuntimeRole
	IdentityPlatform Platform
	Capabilities     []RuntimeCapability
	RiskLevel        RuntimeRiskLevel
}

func (n RuntimeNode) HasCapability(c RuntimeCapability) bool {
	for _, capability := range n.Capabilities {
		if capability == c {
			return true
		}
	}

	return false
}

type RuntimeEdgeKind string

const (
	EdgeHosts   RuntimeEdgeKind = "hosts"
	EdgeBridges RuntimeEdgeKind = "bridges"
	EdgeRoutes  RuntimeEdgeKind = "routes"
	EdgeAdapts  RuntimeEdgeKind = "adapts"
)

type RuntimeEdge struct {
	From RuntimeNodeID
	To   RuntimeNodeID
	Kind RuntimeEdgeKind
	Risk RuntimeRiskLevel
}

type RuntimeTopology struct {
	PrimaryNode RuntimeNodeID
	Nodes       []RuntimeNode
	Edges       []RuntimeEdge
}

func (t *RuntimeTopology) Resolved() bool {
	return t != nil && t.PrimaryNode != RuntimeNodeUnknown && len(t.Nodes) > 0
}

func (t *RuntimeTopology) FindNode(id RuntimeNodeID) (RuntimeNode, bool) {
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

func (t *RuntimeTopology) HasCapability(c RuntimeCapability) bool {
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

func (t *RuntimeTopology) PrimaryNodeData() (RuntimeNode, bool) {
	if t == nil {
		return RuntimeNode{}, false
	}

	return t.FindNode(t.PrimaryNode)
}

// EdgesFrom returns all edges originating from a given node.
func (t *RuntimeTopology) EdgesFrom(id RuntimeNodeID) []RuntimeEdge {
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
func (t *RuntimeTopology) EdgesTo(id RuntimeNodeID) []RuntimeEdge {
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
func (t *RuntimeTopology) NodesWithCapability(c RuntimeCapability) []RuntimeNode {
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
func (t *RuntimeTopology) PrimaryCapabilities() []RuntimeCapability {
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
