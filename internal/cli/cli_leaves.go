package cli

import (
	"fmt"
	"os"

	"github.com/mclucy/lucy/internal/cli/message"
	"github.com/mclucy/lucy/terminal/style"

	"github.com/spf13/cobra"
)

var leavesCmd = &cobra.Command{
	Use:   "leaves",
	Short: message.LeavesShort,
	Args:  cobra.NoArgs,
	RunE:  actionLeaves,
}

func NewLeavesCommand() *cobra.Command {
	leavesCmd.Flags().Bool(
		"live",
		false,
		"Observe the live workspace instead of reading the lock",
	)
	return leavesCmd
}

func actionLeaves(cmd *cobra.Command, args []string) error {
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	forceLive, _ := cmd.Flags().GetBool("live")
	graph, source, _, err := LoadDependencyData(workDir, forceLive)
	if err != nil {
		return err
	}

	mode := JSONMode(cmd)
	if mode != JSONNone {
		return outputLeavesJSON(graph, source, mode)
	}

	fmt.Printf("Using data from: %s\n\n", source.String())

	leaves := graph.GetLeaves()
	if len(leaves) == 0 {
		fmt.Println("No leaf packages found")
	} else {
		for _, leaf := range leaves {
			fmt.Println(nodeLabel(leaf))
		}
	}

	fmt.Printf("\n(from %s)\n", source.String())
	return nil
}

type leafNode struct {
	ID       string `json:"id"`
	Version  string `json:"version"`
	Provider string `json:"provider,omitempty"`
	Project  string `json:"project,omitempty"`
	Filename string `json:"filename,omitempty"`
	Runtime  string `json:"runtime"`
	Loader   string `json:"loader"`
}

func outputLeavesJSON(
	graph *DependencyGraph,
	source DataSource,
	mode JSONRendering,
) error {
	leaves := graph.GetLeaves()
	jsonLeaves := make([]leafNode, 0, len(leaves))
	for _, leaf := range leaves {
		jsonLeaves = append(
			jsonLeaves, leafNode{
				ID:       leaf.ID,
				Version:  leaf.Version,
				Provider: leaf.Provider,
				Project:  leaf.ProjectID,
				Filename: leaf.Filename,
				Runtime:  leaf.Runtime,
				Loader:   leaf.Loader.String(),
			},
		)
	}

	output := map[string]any{
		"source": source.String(),
		"leaves": jsonLeaves,
	}
	switch mode {
	case JSONCompact:
		style.PrintAsJsonCompact(output)
	default:
		style.PrintAsJson(output)
	}
	return nil
}
