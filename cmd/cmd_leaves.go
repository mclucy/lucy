package cmd

import (
	"fmt"
	"os"

	"github.com/mclucy/lucy/tools"

	"github.com/spf13/cobra"
)

var leavesCmd = &cobra.Command{
	Use:   "leaves",
	Short: "List leaf packages (packages with no dependents)",
	Args:  cobra.NoArgs,
	RunE:  runWithErrorLogging(actionLeaves),
}

func init() {
	leavesCmd.Flags().Bool("live", false, "Probe live server instead of reading lock")
	addJsonFlag(leavesCmd)
	addNoStyleFlag(leavesCmd)
	rootCmd.AddCommand(leavesCmd)
}

func actionLeaves(cmd *cobra.Command, args []string) error {
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
		return outputLeavesJSON(graph, source)
	}

	fmt.Printf("Using data from: %s\n\n", source.String())

	leaves := graph.GetLeaves()
	if len(leaves) == 0 {
		fmt.Println("No leaf packages found")
	} else {
		for _, leaf := range leaves {
			label := fmt.Sprintf("%s@%s", leaf.ID, leaf.Version)
			if leaf.Source != "" {
				label += fmt.Sprintf(" (%s)", leaf.Source)
			}
			if leaf.Optional {
				label += " [optional]"
			}
			if leaf.Embedded {
				label += " [embedded]"
			}
			fmt.Println(label)
		}
	}

	fmt.Printf("\n(from %s)\n", source.String())
	return nil
}

type leafNode struct {
	ID       string `json:"id"`
	Version  string `json:"version"`
	Source   string `json:"source,omitempty"`
	Optional bool   `json:"optional,omitempty"`
	Embedded bool   `json:"embedded,omitempty"`
}

func outputLeavesJSON(graph *DependencyGraph, source DataSource) error {
	leaves := graph.GetLeaves()
	jsonLeaves := make([]leafNode, 0, len(leaves))
	for _, leaf := range leaves {
		jsonLeaves = append(jsonLeaves, leafNode{
			ID:       leaf.ID,
			Version:  leaf.Version,
			Source:   leaf.Source,
			Optional: leaf.Optional,
			Embedded: leaf.Embedded,
		})
	}

	output := map[string]interface{}{
		"source": source.String(),
		"leaves": jsonLeaves,
	}
	tools.PrintAsJson(output)
	return nil
}
