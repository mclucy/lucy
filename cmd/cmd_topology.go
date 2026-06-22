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
	longOut, _ := cmd.Flags().GetBool(flagLongName)
	noStyle, _ := cmd.Flags().GetBool(flagNoStyleName)

	info := workspace.ServerInfo()

	if info.Runtime == nil || info.Runtime.Topology == nil {
		return fmt.Errorf("no server runtime detected in current directory")
	}

	topology := info.Runtime.Topology

	if !topology.Resolved() {
		return fmt.Errorf("server topology is unresolved")
	}

	mermaidSource := buildMermaidTopology(topology, longOut)

	if jsonOut {
		style.PrintAsJson(map[string]any{
			"game_version": info.Runtime.GameVersion.String(),
			"primary_node": topology.PrimaryNode,
			"nodes":        topology.Nodes,
			"edges":        topology.Edges,
			"mermaid":      mermaidSource,
		})
		return nil
	}

	rendered := renderTopologyASCII(topology, "LR", noStyle, longOut)
	fmt.Fprintln(cmd.OutOrStdout(), rendered)
	return nil
}

// buildMermaidTopology converts a RuntimeTopology into a Mermaid graph source
// string that mermaid-ascii can render.
func buildMermaidTopology(
	topology *types.RuntimeTopology,
	longOut bool,
) string {
	var b strings.Builder
	b.WriteString("graph TD\n")

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
			fmt.Fprintf(&b, "  %s -->|\"%s\"| %s\n", fromID, verb, toID)
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
		label += " (primary)"
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

	return strings.Join(parts, "\\n")
}

func capabilitiesLabel(caps []types.RuntimeCapability) string {
	labels := make([]string, len(caps))
	for i, c := range caps {
		labels[i] = string(c)
	}
	return strings.Join(labels, ", ")
}
