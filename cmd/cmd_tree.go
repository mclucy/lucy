package cmd

import (
	"fmt"
	"os"

	"github.com/mclucy/lucy/tools"

	"github.com/spf13/cobra"
)

var treeCmd = &cobra.Command{
	Use:   "tree",
	Short: "Display dependency tree structure",
	Args:  cobra.NoArgs,
	RunE:  runWithErrorLogging(actionTree),
}

func init() {
	treeCmd.Flags().Bool("live", false, "Probe live server instead of reading lock")
	treeCmd.Flags().Int("depth", 0, "Limit dependency tree depth (0 = unlimited)")
	addJsonFlag(treeCmd)
	addNoStyleFlag(treeCmd)
	rootCmd.AddCommand(treeCmd)
}

func actionTree(cmd *cobra.Command, args []string) error {
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	forceLive, _ := cmd.Flags().GetBool("live")
	graph, source, err := LoadDependencyData(workDir, forceLive)
	if err != nil {
		return err
	}

	jsonOut, _ := cmd.Flags().GetBool(flagJsonName)
	if jsonOut {
		return outputTreeJSON(graph, source)
	}

	maxDepth, _ := cmd.Flags().GetInt("depth")
	fmt.Printf("Using data from: %s\n\n", source.String())

	roots := graph.GetRoots()
	for i, root := range roots {
		isLast := i == len(roots)-1
		visited := make(map[string]bool)
		printTree(root, 0, isLast, "", visited, maxDepth)
	}

	fmt.Printf("\n(from %s)\n", source.String())
	return nil
}

func printTree(node *GraphNode, depth int, isLast bool, prefix string, visited map[string]bool, maxDepth int) {
	branch := "├── "
	if isLast {
		branch = "└── "
	}

	label := fmt.Sprintf("%s@%s", node.ID, node.Version)
	if node.Source != "" {
		label += fmt.Sprintf(" (%s)", node.Source)
	}
	if node.Optional {
		label += " [optional]"
	}
	if node.Embedded {
		label += " [embedded]"
	}

	if visited[node.ID] {
		fmt.Printf("%s%s%s [shown above]\n", prefix, branch, label)
		return
	}

	fmt.Printf("%s%s%s\n", prefix, branch, label)

	if maxDepth > 0 && depth >= maxDepth {
		return
	}

	visited[node.ID] = true

	childPrefix := prefix + "│   "
	if isLast {
		childPrefix = prefix + "    "
	}

	for i, child := range node.Children {
		printTree(child, depth+1, i == len(node.Children)-1, childPrefix, visited, maxDepth)
	}

	delete(visited, node.ID)
}

type treeNode struct {
	ID       string     `json:"id"`
	Version  string     `json:"version"`
	Source   string     `json:"source,omitempty"`
	Optional bool       `json:"optional,omitempty"`
	Embedded bool       `json:"embedded,omitempty"`
	Children []*treeNode `json:"children,omitempty"`
}

func outputTreeJSON(graph *DependencyGraph, source DataSource) error {
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
	tools.PrintAsJson(output)
	return nil
}

func buildJSONNode(node *GraphNode, visited map[string]bool) *treeNode {
	t := &treeNode{
		ID:       node.ID,
		Version:  node.Version,
		Source:   node.Source,
		Optional: node.Optional,
		Embedded: node.Embedded,
	}

	if visited[node.ID] {
		return t
	}

	visited[node.ID] = true
	for _, child := range node.Children {
		t.Children = append(t.Children, buildJSONNode(child, visited))
	}
	delete(visited, node.ID)

	return t
}
