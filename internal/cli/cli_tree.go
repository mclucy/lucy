package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/internal/cli/help"
	"github.com/spf13/cobra"
)

var treeJSON JSONResult[treeOutput]

func NewTreeCommand(contracts *JSONContracts) *cobra.Command {
	treeCmd := &cobra.Command{
		Use:   "tree",
		Short: help.TreeShort,
		Args:  cobra.NoArgs,
		RunE:  actionTree,
	}
	treeJSON.Bind(contracts, treeCmd)
	treeCmd.Flags().Bool(
		"live",
		false,
		"Observe the live workspace instead of reading the lock",
	)
	treeCmd.Flags().Int(
		"depth",
		0,
		"Limit dependency tree depth (0 = unlimited)",
	)
	return treeCmd
}

func actionTree(cmd *cobra.Command, args []string) error {
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	forceLive, _ := cmd.Flags().GetBool("live")
	output := NewQueryOutput(cmd)
	graph, origin, _, err := LoadDependencyData(workDir, forceLive, output)
	if err != nil {
		return err
	}

	if output.JSON() {
		return treeJSON.Write(output, treeDocument(graph, origin, output.Warnings()))
	}

	maxDepth, _ := cmd.Flags().GetInt("depth")
	human := output.Human()
	fmt.Fprintf(human, "Using data from: %s\n\n", origin.String())

	roots := graph.GetRoots()
	for i, root := range roots {
		printTree(human, root, 0, i == len(roots)-1, "", make(map[string]bool), maxDepth)
	}

	fmt.Fprintf(human, "\n(from %s)\n", origin.String())
	return nil
}

// nodeLabel renders one graph node. Node identity is always an explicit
// artifact coordinate or a native module ID; nothing is re-derived by
// parsing the label.
func nodeLabel(node *GraphNode) string {
	label := fmt.Sprintf("%s@%s", node.ID, node.Version)
	if node.Provider != catalog.ProviderUnspecified {
		label += fmt.Sprintf(" (%s)", node.Provider)
	}
	if node.Module {
		label += " [module]"
	}
	return label
}

func printTree(
	w io.Writer,
	node *GraphNode,
	depth int,
	isLast bool,
	prefix string,
	visited map[string]bool,
	maxDepth int,
) {
	branch := "├── "
	if isLast {
		branch = "└── "
	}

	label := nodeLabel(node)
	key := node.key()

	if visited[key] {
		fmt.Fprintf(w, "%s%s%s [shown above]\n", prefix, branch, label)
		return
	}

	fmt.Fprintf(w, "%s%s%s\n", prefix, branch, label)

	if maxDepth > 0 && depth >= maxDepth {
		return
	}

	visited[key] = true

	childPrefix := prefix + "│   "
	if isLast {
		childPrefix = prefix + "    "
	}

	for i, child := range node.Children {
		printTree(w, child, depth+1, i == len(node.Children)-1, childPrefix, visited, maxDepth)
	}

	delete(visited, key)
}

type treeNode struct {
	ID          string      `json:"id"`
	Version     string      `json:"version"`
	Provider    string      `json:"provider,omitempty"`
	Project     string      `json:"project,omitempty"`
	Artifact    string      `json:"artifact,omitempty"`
	Filename    string      `json:"filename,omitempty"`
	Environment string      `json:"environment"`
	Ecosystem   string      `json:"ecosystem"`
	Module      bool        `json:"module,omitzero"`
	Root        bool        `json:"root,omitzero"`
	Children    []*treeNode `json:"children,omitempty"`
}

// treeOutput is the complete machine-readable `tree` result.
type treeOutput struct {
	Origin   string      `json:"origin"`
	Roots    []*treeNode `json:"roots"`
	Warnings []string    `json:"warnings,omitempty"`
}

func treeDocument(
	graph *DependencyGraph,
	origin GraphOrigin,
	warnings []string,
) treeOutput {
	visited := make(map[string]bool)
	roots := graph.GetRoots()
	jsonRoots := make([]*treeNode, 0, len(roots))
	for _, root := range roots {
		jsonRoots = append(jsonRoots, buildJSONNode(root, visited))
	}

	return treeOutput{
		Origin:   origin.String(),
		Roots:    jsonRoots,
		Warnings: warnings,
	}
}

func buildJSONNode(node *GraphNode, visited map[string]bool) *treeNode {
	provider := ""
	if node.Provider != catalog.ProviderUnspecified {
		provider = node.Provider.String()
	}
	t := &treeNode{
		ID:          node.ID,
		Version:     node.Version,
		Provider:    provider,
		Project:     node.ProjectID,
		Artifact:    node.Artifact,
		Filename:    node.Filename,
		Environment: string(node.Environment),
		Ecosystem:   node.Ecosystem.String(),
		Module:      node.Module,
		Root:        node.Root,
	}

	key := node.key()
	if visited[key] {
		return t
	}

	visited[key] = true
	for _, child := range node.Children {
		t.Children = append(t.Children, buildJSONNode(child, visited))
	}
	delete(visited, key)

	return t
}
