package cmd

import (
	"fmt"
	"strings"

	"github.com/mclucy/lucy/probe"
	"github.com/mclucy/lucy/tools"
	"github.com/mclucy/lucy/tui"
	"github.com/mclucy/lucy/types"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Display basic information of the current server",
	RunE:  runWithErrorLogging(actionStatus),
}

func init() {
	addJsonFlag(statusCmd)
	addLongFlag(statusCmd)
	rootCmd.AddCommand(statusCmd)
}

func actionStatus(cmd *cobra.Command, args []string) error {
	serverInfo := probe.ServerInfo()
	json, _ := cmd.Flags().GetBool(flagJsonName)
	long, _ := cmd.Flags().GetBool(flagLongName)
	noStyle, _ := cmd.Flags().GetBool(flagNoStyleName)
	if json {
		tools.PrintAsJson(serverInfo)
	} else {
		tui.Flush(generateStatusOutput(&serverInfo, long, noStyle))
	}
	return nil
}

func generateStatusOutput(
	data *probe.Workspace,
	longOutput bool,
	noStyle bool,
) (output *tui.Data) {
	packageNameOutput := tools.Ternary(
		longOutput,
		func(pkg types.Package) string { return pkg.Id.StringFull() },
		func(pkg types.Package) string { return pkg.Id.Name.String() },
	)

	if data.Runtime == nil {
		return &tui.Data{
			Fields: []tui.Field{
				&tui.FieldAnnotation{
					Annotation: "(No server found)",
				},
			},
		}
	}

	output = &tui.Data{Fields: []tui.Field{}}
	serverPlatform := data.Runtime.DerivedModLoader()
	hasMcdr := data.Environments.Mcdr != nil
	hasLucy := data.Environments.Lucy != nil
	primaryNode, hasPrimaryNode := topologyPrimaryNodeData(data.Runtime.Topology)

	// logo display strategy:
	// custom client > mod loader > mcdr > lucy > vanilla
	var logoPlatform types.PlatformId
	if serverPlatform == types.PlatformVanilla {
		if hasMcdr {
			logoPlatform = types.PlatformMCDR
		} else if hasLucy {
			// logoPlatform =
			// lucy is not supposed to be a platform, needs refactor
			// also need structural support for all other custom server clients
		} else {
			logoPlatform = types.PlatformVanilla
		}
	} else if serverPlatform.IsModding() {
		output.Fields = append(
			output.Fields,
			&tui.FieldLogo{
				Platform: logoPlatform,
				NoColor:  noStyle,
			},
		)
	}

	output.Fields = append(
		output.Fields,
		&tui.FieldAnnotatedShortText{
			Title:      "Game",
			Text:       data.Runtime.GameVersion.String(),
			Annotation: data.Runtime.PrimaryEntrance,
		},
	)

	if data.Activity != nil {
		output.Fields = append(
			output.Fields, &tui.FieldAnnotatedShortText{
				Title: "Activity",
				Text: tools.Ternary(
					data.Activity.Active,
					"Active",
					"Inactive",
				),
				Annotation: tools.Ternary(
					data.Activity.Active,
					fmt.Sprintf("PID %d", data.Activity.Pid),
					"",
				),
			},
		)
	} else {
		output.Fields = append(
			output.Fields, &tui.FieldShortText{
				Title: "Activity",
				Text:  tools.Dim("(Unknown)"),
			},
		)
	}

	// Show modding platform if detected, even if no mods found, to differentiate
	// between modded and vanilla servers
	if platformLabel := statusRuntimePlatformLabel(
		data.Runtime.Topology,
		data.Packages,
		serverPlatform,
		hasPrimaryNode,
		primaryNode,
	); platformLabel != "" {
		output.Fields = append(
			output.Fields, &tui.FieldAnnotatedShortText{
				Title:      "Platform",
				Text:       platformLabel,
				Annotation: data.Runtime.DerivedLoaderVersion(),
			},
		)
	}

	if topologyField := statusTopologyField(
		data.Runtime.Topology,
		hasPrimaryNode,
		primaryNode,
	); topologyField != nil {
		output.Fields = append(output.Fields, topologyField)
	}

	// If topology is resolved and has meaningful risk, show it.
	if riskLevel := statusEffectiveRiskLevel(
		data.Runtime.Topology,
		hasPrimaryNode,
		primaryNode,
	); riskLevel > types.RiskNone {
		output.Fields = append(
			output.Fields, &tui.FieldShortText{
				Title: "Risk",
				Text:  topologyRiskLabel(riskLevel, noStyle),
			},
		)
	}

	showMods := false
	if data.Runtime.Topology != nil && data.Runtime.Topology.Resolved() {
		showMods = data.Runtime.Topology.HasCapability(types.CapabilityFabricMods) ||
			data.Runtime.Topology.HasCapability(types.CapabilityForgeMods) ||
			data.Runtime.Topology.HasCapability(types.CapabilityNeoforgeMods)
	}

	// Collect mod/plugin names and paths for later use. This is to avoid
	// traversing the package list multiple times, which can be costly when
	// there are many packages.
	var modNames []string
	var modPaths []string
	var mcdrPlugins []string
	if showMods {
		modNames = make([]string, 0, len(data.Packages))
		modPaths = make([]string, 0, len(data.Packages))
	}
	if hasMcdr {
		mcdrPlugins = make([]string, 0, len(data.Packages))
	}
	if showMods || hasMcdr {
		for _, p := range data.Packages {
			if p.Id.IsIdentityPackage() {
				continue
			}
			packagePlatform := p.Id.Platform
			if showMods && packagePlatform == serverPlatform {
				modNames = append(modNames, packageNameOutput(p))
				if p.Local != nil {
					modPaths = append(modPaths, p.Local.Path)
				}
			}
			if hasMcdr && packagePlatform == types.PlatformMCDR {
				mcdrPlugins = append(mcdrPlugins, packageNameOutput(p))
			}
		}
	}

	// Modding related fields only shown when modding platform detected
	if showMods {
		modListTitle := tools.Ternary(
			noStyle,
			"Mods",
			"└── Mods",
		)
		if len(modNames) == 0 {
			output.Fields = append(
				output.Fields, &tui.FieldShortText{
					Title: modListTitle,
					Text:  tools.Dim("(None)"),
				},
			)
		} else {
			output.Fields = append(
				output.Fields,
				tools.Ternary[tui.Field](
					longOutput,
					&tui.FieldMultiAnnotatedShortText{
						Title:       modListTitle,
						Texts:       modNames,
						Annotations: modPaths,
						ShowTotal:   true,
					},
					&tui.FieldDynamicColumnLabels{
						Title:     modListTitle,
						Labels:    modNames,
						MaxLines:  0,
						ShowTotal: true,
					},
				),
			)
		}
	}

	showPlugins := false
	if data.Runtime.Topology != nil && data.Runtime.Topology.Resolved() {
		showPlugins = data.Runtime.Topology.HasCapability(types.CapabilityBukkitPlugins)
	}

	// List plugins if server can load plugins
	if showPlugins {
		pluginNames := make([]string, 0, len(data.Packages))
		for _, p := range data.Packages {
			if p.Id.IsIdentityPackage() {
				continue
			}
			if p.Id.Platform == types.PlatformBukkit {
				pluginNames = append(pluginNames, packageNameOutput(p))
			}
		}

		pluginListTitle := tools.Ternary(
			noStyle,
			"Plugins",
			"└── Plugins",
		)

		if len(pluginNames) == 0 {
			output.Fields = append(
				output.Fields, &tui.FieldShortText{
					Title: pluginListTitle,
					Text:  tools.Dim("(None)"),
				},
			)
		} else {
			output.Fields = append(
				output.Fields, &tui.FieldDynamicColumnLabels{
					Title:     pluginListTitle,
					Labels:    pluginNames,
					MaxLines:  0,
					ShowTotal: true,
				},
			)
		}
	}

	// List MCDR plugins if MCDR environment detected
	if hasMcdr {
		mcdrPluginListTitle := tools.Ternary(
			noStyle,
			"MCDR Plugins",
			"└── Plugins",
		)

		// Tell users that MCDR is installed
		output.Fields = append(
			output.Fields, &tui.FieldShortText{
				Title: "MCDR",
				Text: "Installed" + tools.Ternary(
					noStyle,
					"",
					tools.Green(" ✓"),
				),
			},
		)

		if len(mcdrPlugins) == 0 {
			output.Fields = append(
				output.Fields, &tui.FieldShortText{
					Title: mcdrPluginListTitle,
					Text:  tools.Dim("(None)"),
				},
			)
		} else {
			output.Fields = append(
				output.Fields, &tui.FieldDynamicColumnLabels{
					Title:     mcdrPluginListTitle,
					Labels:    mcdrPlugins,
					MaxLines:  0,
					ShowTotal: true,
				},
			)
		}
	}

	return output
}

func topologyPrimaryNodeData(topology *types.RuntimeTopology) (
	types.RuntimeNode,
	bool,
) {
	if topology == nil || !topology.Resolved() {
		return types.RuntimeNode{}, false
	}

	return topology.PrimaryNodeData()
}

func statusRuntimePlatformLabel(
	topology *types.RuntimeTopology,
	packages []types.Package,
	fallback types.PlatformId,
	hasPrimaryNode bool,
	primaryNode types.RuntimeNode,
) string {
	label := ""
	if hasPrimaryNode {
		if primaryNode.Role != types.RuntimeRoleHybrid {
			if platform := types.DeclaredModdingPlatformForNode(primaryNode.ID); platform != types.PlatformNone && platform != types.PlatformMinecraft {
				label = platform.Title()
			}
		}

		if label == "" {
			if nodeLabel := runtimeNodeLabel(primaryNode.ID); nodeLabel != "" && nodeLabel != "Minecraft" {
				label = nodeLabel
			}
		}
	}

	if label == "" && topology != nil && topology.Resolved() && fallback != types.PlatformMinecraft && fallback != types.PlatformAny {
		label = fallback.Title()
	}

	if label == "" {
		return ""
	}

	if addons := statusPackageAddonLabels(
		packages,
		primaryNode,
	); len(addons) > 0 {
		label += " + " + strings.Join(addons, " + ")
	}

	if extras := runtimeTopologyAddonLabels(
		topology,
		primaryNode.ID,
	); len(extras) > 0 {
		label += " + " + strings.Join(extras, " + ")
	}

	return label
}

func statusPackageAddonLabels(
	packages []types.Package,
	primaryNode types.RuntimeNode,
) []string {
	labels := make([]string, 0, len(packages))
	seen := map[string]struct{}{}
	for _, pkg := range packages {
		label := packageRuntimeLabel(pkg)
		if label == "" || label == runtimeNodeLabel(primaryNode.ID) {
			continue
		}
		if _, exists := seen[label]; exists {
			continue
		}
		seen[label] = struct{}{}
		labels = append(labels, label)
	}
	return labels
}

func packageRuntimeLabel(pkg types.Package) string {
	switch pkg.Id.Platform {
	case types.PlatformFabric:
		return "Fabric"
	case types.PlatformForge:
		return "Forge"
	case types.PlatformNeoforge:
		return "NeoForge"
	case types.PlatformMCDR:
		return "MCDR"
	case types.PlatformId("paper"):
		return "Paper"
	case types.PlatformId("bukkit"):
		return "Bukkit"
	case types.PlatformId("folia"):
		return "Folia"
	case types.PlatformId("leaves"):
		return "Leaves"
	case types.PlatformId("velocity"):
		return "Velocity"
	case types.PlatformId("bungeecord"):
		return "BungeeCord"
	case types.PlatformId("waterfall"):
		return "Waterfall"
	case types.PlatformId("sponge"):
		return "Sponge"
	case types.PlatformAny:
		switch pkg.Id.Name.String() {
		case "connector":
			return "Connector"
		case "kilt":
			return "Kilt"
		case "geyser":
			return "Geyser"
		case "sponge":
			return "Sponge"
		case "arclight":
			return "Arclight"
		case "youer":
			return "Youer"
		}
	}

	return ""
}

func statusTopologyField(
	topology *types.RuntimeTopology,
	hasPrimaryNode bool,
	primaryNode types.RuntimeNode,
) tui.Field {
	if topology == nil {
		return nil
	}

	if !topology.Resolved() {
		return &tui.FieldShortText{
			Title: "Topology",
			Text:  tools.Dim("(Unresolved)"),
		}
	}

	if !hasPrimaryNode {
		return &tui.FieldShortText{
			Title: "Topology",
			Text:  tools.Dim("(Unknown)"),
		}
	}

	roleLabel := runtimeRoleLabel(primaryNode.Role)
	if roleLabel == "Mod loader" || roleLabel == "Plugin core" || roleLabel == "Vanilla" {
		return nil
	}
	if roleLabel == "" {
		return nil
	}

	annotation := runtimeTopologyRelationLabel(topology, primaryNode)
	if annotation == "" {
		return &tui.FieldShortText{
			Title: "Topology",
			Text:  roleLabel,
		}
	}

	return &tui.FieldAnnotatedShortText{
		Title:      "Topology",
		Text:       roleLabel,
		Annotation: annotation,
	}
}

// statusEffectiveRiskLevel derives a display risk from the primary runtime node and
// its directly connected neighboring nodes. Edges themselves are structural only.
func statusEffectiveRiskLevel(
	topology *types.RuntimeTopology,
	hasPrimaryNode bool,
	primaryNode types.RuntimeNode,
) types.RuntimeRiskLevel {
	effective := types.RiskNone
	if hasPrimaryNode {
		effective = primaryNode.RiskLevel
	}

	if topology == nil {
		return effective
	}

	for _, edge := range topology.EdgesFrom(topology.PrimaryNode) {
		if target, ok := topology.FindNode(edge.To); ok && target.RiskLevel > effective {
			effective = target.RiskLevel
		}
	}

	for _, edge := range topology.EdgesTo(topology.PrimaryNode) {
		if source, ok := topology.FindNode(edge.From); ok && source.RiskLevel > effective {
			effective = source.RiskLevel
		}
	}

	return effective
}

func runtimeTopologyRelationLabel(
	topology *types.RuntimeTopology,
	primaryNode types.RuntimeNode,
) string {
	switch primaryNode.Role {
	case types.RuntimeRoleProxy:
		if targets := runtimeTopologyTargets(
			topology,
			primaryNode.ID,
		); len(targets) > 0 {
			return "proxies to " + strings.Join(targets, ", ")
		}
		return "proxies to backends"
	case types.RuntimeRoleHybrid:
		if targets := runtimeTopologyTargets(
			topology,
			primaryNode.ID,
		); len(targets) > 0 {
			return "hosts " + strings.Join(targets, ", ")
		}
		return "hybrid runtime"
	case types.RuntimeRoleBridge:
		if targets := runtimeTopologyTargets(
			topology,
			primaryNode.ID,
		); len(targets) > 0 {
			return "hosts compatibility layer"
		}
		return "compatibility layer"
	case types.RuntimeRoleProtocolBridge:
		if targets := runtimeTopologyTargets(
			topology,
			primaryNode.ID,
		); len(targets) > 0 {
			return "provides protocol compatibility for " + strings.Join(
				targets,
				", ",
			)
		}
		return "protocol bridge"
	default:
		return ""
	}
}

func runtimeTopologyTargets(
	topology *types.RuntimeTopology,
	nodeID types.RuntimeNodeID,
) []string {
	if topology == nil {
		return nil
	}

	targets := make([]string, 0, 2)
	seen := make(map[string]struct{}, 2)
	for _, edge := range topology.EdgesFrom(nodeID) {
		switch edge.Verb {
		case types.EdgeHosts, types.EdgeProxies:
			// keep - these point to meaningful targets
		default:
			continue
		}
		if target, ok := topology.FindNode(edge.To); ok {
			label := runtimeNodeLabel(target.ID)
			if label == "" {
				continue
			}
			if _, exists := seen[label]; exists {
				continue
			}
			seen[label] = struct{}{}
			targets = append(targets, label)
		}
	}
	return targets
}

func runtimeTopologyAddonLabels(
	topology *types.RuntimeTopology,
	primaryNodeID types.RuntimeNodeID,
) []string {
	if topology == nil {
		return nil
	}

	labels := make([]string, 0, len(topology.Nodes))
	seen := map[string]struct{}{}
	for _, node := range topology.Nodes {
		if node.ID == primaryNodeID {
			continue
		}

		if node.Role == types.RuntimeRoleModLoader || node.Role == types.RuntimeRoleVanilla {
			continue
		}

		label := runtimeNodeLabel(node.ID)
		if label == "" || label == "Vanilla" {
			continue
		}
		if _, exists := seen[label]; exists {
			continue
		}

		seen[label] = struct{}{}
		labels = append(labels, label)
	}

	return labels
}

func runtimeRoleLabel(role types.RuntimeRole) string {
	switch role {
	case types.RuntimeRoleModLoader:
		return "Mod loader"
	case types.RuntimeRolePluginCore:
		return "Plugin core"
	case types.RuntimeRoleHybrid:
		return "Hybrid"
	case types.RuntimeRoleProxy:
		return "Proxy"
	case types.RuntimeRoleBridge:
		return "Bridge"
	case types.RuntimeRoleProtocolBridge:
		return "Protocol bridge"
	case types.RuntimeRoleVanilla:
		return "Vanilla"
	default:
		return ""
	}
}

func runtimeNodeLabel(id types.RuntimeNodeID) string {
	switch id {
	case types.RuntimeNodeMinecraft:
		return "Vanilla"
	case types.RuntimeNodeFabric:
		return "Fabric"
	case types.RuntimeNodeForge:
		return "Forge"
	case types.RuntimeNodeNeoforge:
		return "NeoForge"
	case types.RuntimeNodeMCDR:
		return "MCDR"
	case types.RuntimeNodePaper:
		return "Paper"
	case types.RuntimeNodeSpigot:
		return "Spigot"
	case types.RuntimeNodeBukkit:
		return "Bukkit"
	case types.RuntimeNodeFolia:
		return "Folia"
	case types.RuntimeNodeLeaves:
		return "Leaves"
	case types.RuntimeNodeSponge:
		return "Sponge"
	case types.RuntimeNodeArclight:
		return "Arclight"
	case types.RuntimeNodeYouer:
		return "Youer"
	case types.RuntimeNodeVelocity:
		return "Velocity"
	case types.RuntimeNodeBungeecord:
		return "BungeeCord"
	case types.RuntimeNodeWaterfall:
		return "Waterfall"
	case types.RuntimeNodeGeyserStandalone:
		return "Geyser Standalone"
	case types.RuntimeNodeGeyser:
		return "Geyser"
	case types.RuntimeNodeConnector:
		return "Connector"
	case types.RuntimeNodeKilt:
		return "Kilt"
	default:
		return tools.Capitalize(
			strings.ReplaceAll(
				strings.ReplaceAll(
					string(id),
					"-",
					" ",
				), "_", " ",
			),
		)
	}
}

func topologyRiskLabel(level types.RuntimeRiskLevel, noStyle bool) string {
	switch level {
	case types.RiskLow:
		return "Low"
	case types.RiskMedium:
		return "Medium" + tools.Ternary(noStyle, "", " ⚠")
	case types.RiskHigh:
		return "High" + tools.Ternary(noStyle, "", " ⚠⚠")
	case types.RiskCritical:
		return "Critical" + tools.Ternary(noStyle, "", " ✗")
	default:
		return "None"
	}
}
