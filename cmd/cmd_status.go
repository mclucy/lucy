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
	data *types.ServerInfo,
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
	var logoPlatform types.Platform
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
	if platformLabel := statusRuntimePlatformLabel(data.Runtime.Topology, data.Packages, serverPlatform, hasPrimaryNode, primaryNode); platformLabel != "" {
		output.Fields = append(
			output.Fields, &tui.FieldAnnotatedShortText{
				Title:      "Platform",
				Text:       platformLabel,
				Annotation: data.Runtime.DerivedLoaderVersion(),
			},
		)
	}

	if topologyField := statusTopologyField(data.Runtime.Topology, hasPrimaryNode, primaryNode); topologyField != nil {
		output.Fields = append(output.Fields, topologyField)
	}

	// If topology is resolved and has meaningful risk, show it.
	if riskLevel := statusEffectiveRiskLevel(data.Runtime.Topology, hasPrimaryNode, primaryNode); riskLevel > types.RiskNone {
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

func topologyPrimaryNodeData(topology *types.RuntimeTopology) (types.RuntimeNode, bool) {
	if topology == nil || !topology.Resolved() {
		return types.RuntimeNode{}, false
	}

	return topology.PrimaryNodeData()
}

func statusRuntimePlatformLabel(
	topology *types.RuntimeTopology,
	packages []types.Package,
	fallback types.Platform,
	hasPrimaryNode bool,
	primaryNode types.RuntimeNode,
) string {
	label := ""
	if hasPrimaryNode {
		if primaryNode.IdentityPlatform.Valid() && primaryNode.IdentityPlatform != types.PlatformAny && primaryNode.IdentityPlatform != types.PlatformMinecraft {
			label = primaryNode.IdentityPlatform.Title()
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

	if addons := statusPackageAddonLabels(packages, primaryNode); len(addons) > 0 {
		label += " + " + strings.Join(addons, " + ")
	}

	if extras := runtimeTopologyAddonLabels(topology, primaryNode.ID); len(extras) > 0 {
		label += " + " + strings.Join(extras, " + ")
	}

	return label
}

func statusPackageAddonLabels(packages []types.Package, primaryNode types.RuntimeNode) []string {
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
	case types.Platform("paper"):
		return "Paper"
	case types.Platform("bukkit"):
		return "Bukkit"
	case types.Platform("folia"):
		return "Folia"
	case types.Platform("leaves"):
		return "Leaves"
	case types.Platform("velocity"):
		return "Velocity"
	case types.Platform("bungeecord"):
		return "BungeeCord"
	case types.Platform("waterfall"):
		return "Waterfall"
	case types.Platform("sponge"):
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
		if edge.Kind != types.EdgeBridges && edge.Kind != types.EdgeRoutes && edge.Kind != types.EdgeAdapts {
			continue
		}
		if edge.Risk > effective {
			effective = edge.Risk
		}
	}

	for _, edge := range topology.EdgesTo(topology.PrimaryNode) {
		if edge.Kind != types.EdgeBridges && edge.Kind != types.EdgeRoutes && edge.Kind != types.EdgeAdapts {
			continue
		}
		if edge.Risk > effective {
			effective = edge.Risk
		}
	}

	return effective
}

func runtimeTopologyRelationLabel(topology *types.RuntimeTopology, primaryNode types.RuntimeNode) string {
	switch primaryNode.Role {
	case types.RuntimeRoleProxy:
		if targets := runtimeTopologyTargets(topology, primaryNode.ID); len(targets) > 0 {
			return "routes to " + strings.Join(targets, ", ")
		}
		return "routes traffic"
	case types.RuntimeRoleHybrid:
		if targets := runtimeTopologyTargets(topology, primaryNode.ID); len(targets) > 0 {
			return "bridges " + strings.Join(targets, ", ")
		}
		return "hybrid runtime"
	case types.RuntimeRoleBridge:
		if targets := runtimeTopologyTargets(topology, primaryNode.ID); len(targets) > 0 {
			return "bridges to " + strings.Join(targets, ", ")
		}
		return "bridge layer"
	case types.RuntimeRoleProtocolBridge:
		if targets := runtimeTopologyTargets(topology, primaryNode.ID); len(targets) > 0 {
			return "protocol bridge to " + strings.Join(targets, ", ")
		}
		return "protocol bridge"
	default:
		return ""
	}
}

func runtimeTopologyTargets(topology *types.RuntimeTopology, nodeID types.RuntimeNodeID) []string {
	if topology == nil {
		return nil
	}

	targets := make([]string, 0, 2)
	seen := make(map[string]struct{}, 2)
	for _, edge := range topology.EdgesFrom(nodeID) {
		if edge.Kind != types.EdgeBridges && edge.Kind != types.EdgeRoutes && edge.Kind != types.EdgeAdapts {
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

func runtimeTopologyAddonLabels(topology *types.RuntimeTopology, primaryNodeID types.RuntimeNodeID) []string {
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
	case probe.RuntimeNodeMinecraft:
		return "Vanilla"
	case probe.RuntimeNodeFabric:
		return "Fabric"
	case probe.RuntimeNodeForge:
		return "Forge"
	case probe.RuntimeNodeNeoforge:
		return "NeoForge"
	case probe.RuntimeNodeMCDR:
		return "MCDR"
	case probe.RuntimeNodePaper:
		return "Paper"
	case probe.RuntimeNodeSpigot:
		return "Spigot"
	case probe.RuntimeNodeBukkit:
		return "Bukkit"
	case probe.RuntimeNodeFolia:
		return "Folia"
	case probe.RuntimeNodeLeaves:
		return "Leaves"
	case probe.RuntimeNodeSponge:
		return "Sponge"
	case probe.RuntimeNodeArclight:
		return "Arclight"
	case probe.RuntimeNodeYouer:
		return "Youer"
	case probe.RuntimeNodeVelocity:
		return "Velocity"
	case probe.RuntimeNodeBungeecord:
		return "BungeeCord"
	case probe.RuntimeNodeWaterfall:
		return "Waterfall"
	case probe.RuntimeNodeGeyserStandalone:
		return "Geyser Standalone"
	case probe.RuntimeNodeGeyser:
		return "Geyser"
	case probe.RuntimeNodeConnector:
		return "Connector"
	case probe.RuntimeNodeKilt:
		return "Kilt"
	default:
		return tools.Capitalize(strings.ReplaceAll(strings.ReplaceAll(string(id), "-", " "), "_", " "))
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
