package status

import (
	"fmt"
	"strings"

	"github.com/mclucy/lucy/internal/cli"
	"github.com/mclucy/lucy/internal/fn"
	"github.com/mclucy/lucy/tui"
	"github.com/mclucy/lucy/tui/style"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/workspace"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Display basic information of the current server",
	Args:  cobra.NoArgs,
	RunE:  cli.WithErrorLogging(actionStatus),
}

// NewCommand wires and returns the `lucy status` command.
func NewCommand() *cobra.Command {
	cli.AddJSONFlag(statusCmd)
	cli.AddLongFlag(statusCmd)
	return statusCmd
}

func actionStatus(cmd *cobra.Command, args []string) error {
	ws := workspace.New()
	json, _ := cmd.Flags().GetBool(cli.FlagJSON)
	jsonCompact, _ := cmd.Flags().GetBool(cli.FlagJSONCompact)
	long, _ := cmd.Flags().GetBool(cli.FlagLong)
	noStyle, _ := cmd.Flags().GetBool(cli.FlagNoStyle)
	if json || jsonCompact {
		if jsonCompact {
			style.PrintAsJsonCompact(ws)
		} else {
			style.PrintAsJson(ws)
		}
	} else {
		tui.Flush(generateStatusOutput(&ws, long, noStyle))
	}
	return nil
}

func generateStatusOutput(
	data *workspace.Workspace,
	longOutput bool,
	noStyle bool,
) (output *tui.Data) {
	if data.Server == nil {
		return &tui.Data{
			Fields: []tui.Field{
				&tui.FieldAnnotation{
					Annotation: "(No server found)",
				},
			},
		}
	}

	output = &tui.Data{Fields: []tui.Field{}}
	serverPlatform := data.DerivedModLoader()
	modPlatforms := statusModEcosystems(data.Topology, serverPlatform)
	showPlatformQualifiedMods := len(modPlatforms) > 1
	packageNameOutput := func(pkg types.DiscoveredPackage) string {
		if longOutput {
			return pkg.Id.StringFull()
		}
		if showPlatformQualifiedMods {
			return pkg.Id.StringBase()
		}
		return pkg.Id.Name.String()
	}
	hasMcdr := data.Environments.Mcdr != nil
	hasLucy := data.Environments.Lucy != nil
	primaryNode, hasPrimaryNode := topologyPrimaryNodeData(data.Topology)
	showMods := len(modPlatforms) > 0
	showPlugins := data.Topology != nil && data.Topology.Resolved() && data.Topology.HasCapability(types.CapabilityBukkitAPI)
	modNames, modPaths, pluginNames, mcdrPlugins := statusPackageSections(
		data.Packages,
		modPlatforms,
		packageNameOutput,
		showMods,
		showPlugins,
		hasMcdr,
	)

	// logo display strategy:
	// custom client > mod loader > mcdr > lucy > vanilla
	var logoEco types.Ecosystem
	if serverPlatform == types.EcoVanilla {
		if hasMcdr {
			logoEco = types.EcoMcdr
		} else if hasLucy {
			// logoEco =
			// lucy is not supposed to be a platform, needs refactor
			// also need structural support for all other custom server clients
		} else {
			logoEco = types.EcoVanilla
		}
	} else if serverPlatform.IsModding() {
		output.Fields = append(
			output.Fields,
			&tui.FieldLogo{
				Eco:     logoEco,
				NoColor: noStyle,
			},
		)
	}

	output.Fields = append(
		output.Fields,
		&tui.FieldAnnotatedShortText{
			Title:      "Game",
			Text:       data.Server.GameVersion().String(),
			Annotation: data.Server.PrimaryEntrance,
		},
	)

	if data.Activity != nil {
		output.Fields = append(
			output.Fields, &tui.FieldAnnotatedShortText{
				Title: "Activity",
				Text: fn.Ternary(
					data.Activity.Active,
					"Active",
					"Inactive",
				),
				Annotation: fn.Ternary(
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
				Text:  style.Muted("(Unknown)"),
			},
		)
	}

	if platformLabel := statusRuntimeEcosystemLabel(
		data.Topology,
		serverPlatform,
		hasPrimaryNode,
		primaryNode,
	); platformLabel != "" {
		children := make([]tui.TreeNode, 0, 2)
		if showMods {
			children = append(
				children,
				tui.TreeNode{
					Title: "Mods",
					Field: statusPackageListField(
						modNames,
						modPaths,
						longOutput,
					),
				},
			)
		}
		if showPlugins {
			children = append(
				children,
				tui.TreeNode{
					Title: "Plugins",
					Field: statusPackageListField(pluginNames, nil, false),
				},
			)
		}

		output.Fields = append(
			output.Fields, &tui.FieldTree{
				Title:      "Platform",
				Text:       platformLabel,
				Annotation: data.DerivedLoaderVersion(),
				Children:   children,
			},
		)
	}

	if topologyField := statusTopologyField(
		data.Topology,
		hasPrimaryNode,
		primaryNode,
	); topologyField != nil {
		output.Fields = append(output.Fields, topologyField)
	}

	if hasMcdr {
		children := []tui.TreeNode{
			{
				Title: "Plugins",
				Field: statusPackageListField(mcdrPlugins, nil, false),
			},
		}
		output.Fields = append(
			output.Fields, &tui.FieldTree{
				Title: "MCDR",
				Text: "Installed" + fn.Ternary(
					noStyle,
					"",
					style.Success(" ✓"),
				),
				Children: children,
			},
		)
	}

	return output
}

func statusPackageListField(
	names []string,
	paths []string,
	longOutput bool,
) tui.Field {
	if len(names) == 0 {
		return &tui.FieldShortText{Text: style.Muted("(None)")}
	}
	if longOutput {
		return &tui.FieldMultiAnnotatedShortText{
			Texts:       names,
			Annotations: paths,
			ShowTotal:   true,
		}
	}
	return &tui.FieldDynamicColumnLabels{
		Labels:    names,
		MaxLines:  0,
		ShowTotal: true,
	}
}

func statusPackageSections(
	packages []types.DiscoveredPackage,
	modPlatforms map[types.Ecosystem]bool,
	packageNameOutput func(types.DiscoveredPackage) string,
	showMods bool,
	showPlugins bool,
	hasMcdr bool,
) ([]string, []string, []string, []string) {
	modNames := make([]string, 0, len(packages))
	modPaths := make([]string, 0, len(packages))
	pluginNames := make([]string, 0, len(packages))
	mcdrPlugins := make([]string, 0, len(packages))

	for _, pkg := range packages {
		if types.IsIdentityPackage(pkg.Id.PackageRef) {
			continue
		}

		packagePlatform := pkg.Id.Eco
		if showMods && modPlatforms[packagePlatform] {
			modNames = append(modNames, packageNameOutput(pkg))
			if pkg.Path != "" {
				modPaths = append(modPaths, pkg.Path)
			}
		}
		if showPlugins && packagePlatform == types.EcoBukkit {
			pluginNames = append(pluginNames, packageNameOutput(pkg))
		}
		if hasMcdr && packagePlatform == types.EcoMcdr {
			mcdrPlugins = append(mcdrPlugins, packageNameOutput(pkg))
		}
	}

	return modNames, modPaths, pluginNames, mcdrPlugins
}

func statusModEcosystems(
	topology *types.ServerTopology,
	serverPlatform types.Ecosystem,
) map[types.Ecosystem]bool {
	platforms := make(map[types.Ecosystem]bool, 3)
	if topology == nil || !topology.Resolved() {
		return platforms
	}

	if serverPlatform.IsModding() {
		platforms[serverPlatform] = true
	}
	if serverPlatform == types.EcoNeoforge {
		platforms[types.EcoForge] = true
	}
	if topology.HasCapability(types.CapabilityFabricLoader) {
		platforms[types.EcoFabric] = true
	}
	if topology.HasCapability(types.CapabilityForge) {
		platforms[types.EcoForge] = true
	}
	if topology.HasCapability(types.CapabilityNeoforge) {
		platforms[types.EcoNeoforge] = true
	}

	return platforms
}

func topologyPrimaryNodeData(topology *types.ServerTopology) (
	types.RuntimeNode,
	bool,
) {
	if topology == nil || !topology.Resolved() {
		return types.RuntimeNode{}, false
	}

	return topology.PrimaryNodeData()
}

func statusRuntimeEcosystemLabel(
	topology *types.ServerTopology,
	fallback types.Ecosystem,
	hasPrimaryNode bool,
	primaryNode types.RuntimeNode,
) string {
	label := ""
	if hasPrimaryNode {
		if primaryNode.Role != types.RuntimeRoleHybrid {
			if platform := types.DeclaredModdingEcosystemForNode(primaryNode.ID); platform != types.EcoUnspecified && platform != types.EcoMinecraft {
				label = platform.Title()
			}
		}

		if label == "" {
			if nodeLabel := cli.RuntimeNodeLabel(primaryNode.ID); nodeLabel != "" && nodeLabel != "Minecraft" {
				label = nodeLabel
			}
		}
	}

	if label == "" && topology != nil && topology.Resolved() && fallback != types.EcoMinecraft && fallback != types.EcoUnspecified {
		label = fallback.Title()
	}

	if label == "" {
		return ""
	}

	if extras := runtimeTopologyAddonLabels(
		topology,
		primaryNode.ID,
	); len(extras) > 0 {
		label += " + " + strings.Join(extras, " + ")
	}

	return label
}

func statusTopologyField(
	topology *types.ServerTopology,
	hasPrimaryNode bool,
	primaryNode types.RuntimeNode,
) tui.Field {
	if topology == nil {
		return nil
	}

	if !topology.Resolved() {
		return &tui.FieldShortText{
			Title: "Topology",
			Text:  style.Muted("(Unresolved)"),
		}
	}

	if !hasPrimaryNode {
		return &tui.FieldShortText{
			Title: "Topology",
			Text:  style.Muted("(Unknown)"),
		}
	}

	roleLabel := cli.RuntimeRoleLabel(primaryNode.Role)
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

func runtimeTopologyRelationLabel(
	topology *types.ServerTopology,
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
	topology *types.ServerTopology,
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
			label := cli.RuntimeNodeLabel(target.ID)
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
	topology *types.ServerTopology,
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

		label := cli.RuntimeNodeLabel(node.ID)
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
