package cmd

import (
	"fmt"
	"os"

	"github.com/mclucy/lucy/internal/cli"
	"github.com/mclucy/lucy/terminal/style"

	"github.com/spf13/cobra"
)

var treeCmd = &cobra.Command{
	Use:   "tree",
	Short: "Show the resolved package dependency tree",
	Args:  cobra.NoArgs,
	RunE:  cli.WithErrorLogging(actionTree),
}

func init() {
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
	cli.AddJSONFlag(treeCmd)
	cli.AddJSONCompactFlag(treeCmd)
	cli.AddNoStyleFlag(treeCmd)
	rootCmd.AddCommand(treeCmd)
}

func actionTree(cmd *cobra.Command, args []string) error {
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	forceLive, _ := cmd.Flags().GetBool("live")
	graph, source, _, err := cli.LoadDependencyData(workDir, forceLive)
	if err != nil {
		return err
	}

	jsonOut, _ := cmd.Flags().GetBool(cli.FlagJSON)
	jsonCompact, _ := cmd.Flags().GetBool(cli.FlagJSONCompact)

	if jsonOut || jsonCompact {
		return outputTreeJSON(graph, source, jsonCompact)
	}

	maxDepth, _ := cmd.Flags().GetInt("depth")
	fmt.Printf("Using data from: %s\n\n", source.String())

	roots := graph.GetRoots()
	for i, root := range roots {
		printTree(root, 0, i == len(roots)-1, "", make(map[string]bool), maxDepth)
	}

	fmt.Printf("\n(from %s)\n", source.String())
	return nil
}

// nodeLabel renders one graph node. Node identity is always an explicit
// artifact coordinate or a native module ID; nothing is re-derived by
// parsing the label.
func nodeLabel(node *cli.GraphNode) string {
	label := fmt.Sprintf("%s@%s", node.ID, node.Version)
	if node.Provider != "" {
		label += fmt.Sprintf(" (%s)", node.Provider)
	}
	if node.Module {
		label += " [module]"
	}
	return label
}

// nodeKey scopes a node so the same identity in two runtimes or loaders is
// tracked separately while rendering.
func nodeKey(node *cli.GraphNode) string {
	return node.Runtime + "|" + string(node.Loader) + "|" + node.ID
}

func printTree(
	node *cli.GraphNode,
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
	key := nodeKey(node)

	if visited[key] {
		fmt.Printf("%s%s%s [shown above]\n", prefix, branch, label)
		return
	}

	fmt.Printf("%s%s%s\n", prefix, branch, label)

	if maxDepth > 0 && depth >= maxDepth {
		return
	}

	visited[key] = true

	childPrefix := prefix + "│   "
	if isLast {
		childPrefix = prefix + "    "
	}

	for i, child := range node.Children {
		printTree(child, depth+1, i == len(node.Children)-1, childPrefix, visited, maxDepth)
	}

	delete(visited, key)
}

type treeNode struct {
	ID       string      `json:"id"`
	Version  string      `json:"version"`
	Provider string      `json:"provider,omitempty"`
	Project  string      `json:"project,omitempty"`
	Artifact string      `json:"artifact,omitempty"`
	Filename string      `json:"filename,omitempty"`
	Runtime  string      `json:"runtime"`
	Loader   string      `json:"loader"`
	Module   bool        `json:"module,omitempty"`
	Root     bool        `json:"root,omitempty"`
	Children []*treeNode `json:"children,omitempty"`
}

func outputTreeJSON(graph *cli.DependencyGraph, source cli.DataSource, compact bool) error {
	visited := make(map[string]bool)
	roots := graph.GetRoots()
	jsonRoots := make([]*treeNode, 0, len(roots))
	for _, root := range roots {
		jsonRoots = append(jsonRoots, buildJSONNode(root, visited))
	}

	output := map[string]interface{}{
		"source": source.String(),
		"roots":  jsonRoots,
	}
	if compact {
		style.PrintAsJsonCompact(output)
	} else {
		style.PrintAsJson(output)
	}
	return nil
}

func buildJSONNode(node *cli.GraphNode, visited map[string]bool) *treeNode {
	t := &treeNode{
		ID:       node.ID,
		Version:  node.Version,
		Provider: node.Provider,
		Project:  node.ProjectID,
		Artifact: node.Artifact,
		Filename: node.Filename,
		Runtime:  node.Runtime,
		Loader:   node.Loader.String(),
		Module:   node.Module,
		Root:     node.Root,
	}

	key := nodeKey(node)
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
