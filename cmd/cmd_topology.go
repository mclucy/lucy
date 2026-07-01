package cmd

import (
	"fmt"
	"strings"

	"github.com/mclucy/lucy/tui/style"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/workspace"
	"github.com/spf13/cobra"
)

var topologyCmd = &cobra.Command{
	Use:   "topology",
	Short: "Visualize server runtime topology as an ASCII diagram",
	Args:  cobra.NoArgs,
	RunE:  runWithErrorLogging(actionTopology),
}

func init() {
	addJsonFlag(topologyCmd)
	addLongFlag(topologyCmd)
	addNoStyleFlag(topologyCmd)
	rootCmd.AddCommand(topologyCmd)
}

func actionTopology(cmd *cobra.Command, args []string) error {
	jsonOut, _ := cmd.Flags().GetBool(flagJsonName)
	jsonCompact, _ := cmd.Flags().GetBool(flagJsonCompactName)
	longOut, _ := cmd.Flags().GetBool(flagLongName)
	noStyle, _ := cmd.Flags().GetBool(flagNoStyleName)

	info := workspace.New()

	if info.Server == nil || info.Topology == nil {
		return fmt.Errorf("no server runtime detected in current directory")
	}

	topology := info.Topology

	if !topology.Resolved() {
		return fmt.Errorf("server topology is unresolved")
	}

	mermaidSource := buildMermaidTopology(topology, "LR", longOut)

	output := map[string]any{
		"game_version": info.Server.GameVersion.String(),
		"primary_node": topology.PrimaryNode,
		"nodes":        topology.Nodes,
		"edges":        topology.Edges,
		"mermaid":      mermaidSource,
	}

	if jsonOut || jsonCompact {
		if jsonCompact {
			style.PrintAsJsonCompact(output)
		} else {
			style.PrintAsJson(output)
		}
		return nil
	}

	rendered := renderTopologyASCII(topology, "LR", noStyle, longOut)
	fmt.Fprintln(cmd.OutOrStdout(), rendered)
	return nil
}

// buildMermaidTopology converts a RuntimeTopology into a Mermaid graph source
// string that mermaid-ascii can render.
func buildMermaidTopology(
	topology *types.ServerTopology,
	direction string,
	longOut bool,
) string {
	var b strings.Builder
	fmt.Fprintf(&b, "graph %s\n", direction)

	nodeIDs := make(map[types.RuntimeNodeID]string, len(topology.Nodes))
	for i, node := range topology.Nodes {
		id := fmt.Sprintf("node%d", i)
		nodeIDs[node.ID] = id

		label := buildNodeLabel(node, topology.PrimaryNode, longOut)
		fmt.Fprintf(&b, "  %s[\"%s\"]\n", id, label)
	}

	for _, edge := range topology.Edges {
		fromID, okFrom := nodeIDs[edge.From]
		toID, okTo := nodeIDs[edge.To]
		if !okFrom || !okTo {
			continue
		}

		verb := string(edge.Verb)
		if verb != "" {
			fmt.Fprintf(&b, "  %s -->|%s| %s\n", fromID, verb, toID)
		} else {
			fmt.Fprintf(&b, "  %s --> %s\n", fromID, toID)
		}
	}

	return b.String()
}

func buildNodeLabel(
	node types.RuntimeNode,
	primary types.RuntimeNodeID,
	longOut bool,
) string {
	label := runtimeNodeLabel(node.ID)
	if node.ID == primary {
		if longOut {
			label += " (primary)"
		} else {
			label += " *"
		}
	}

	if !longOut {
		return label
	}

	parts := []string{label}
	if role := runtimeRoleLabel(node.Role); role != "" {
		parts = append(parts, role)
	}
	if len(node.Capabilities) > 0 {
		parts = append(parts, capabilitiesLabel(node.Capabilities))
	}
	if node.RiskLevel > types.RiskNone {
		parts = append(parts, topologyRiskLabel(node.RiskLevel, true))
	}

	return strings.Join(parts, "\n")
}

func capabilitiesLabel(caps []types.RuntimeCapability) string {
	labels := make([]string, len(caps))
	for i, c := range caps {
		labels[i] = string(c)
	}
	return strings.Join(labels, ", ")
}
