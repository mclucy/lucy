package cli

import (
	"fmt"
	"os"

	"github.com/mclucy/lucy/internal/cli/help"
	"github.com/mclucy/lucy/terminal/style"

	"github.com/spf13/cobra"
)

var leavesCmd = &cobra.Command{
	Use:   "leaves",
	Short: help.LeavesShort,
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
	graph, origin, _, err := LoadDependencyData(workDir, forceLive)
	if err != nil {
		return err
	}

	mode := JSONMode(cmd)
	if mode != JSONNone {
		return outputLeavesJSON(graph, origin, mode)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Using data from: %s\n\n", origin.String())

	leaves := graph.GetLeaves()
	if len(leaves) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No leaf packages found")
	} else {
		for _, leaf := range leaves {
			fmt.Fprintln(cmd.OutOrStdout(), nodeLabel(leaf))
		}
	}

	fmt.Fprintf(cmd.OutOrStdout(), "\n(from %s)\n", origin.String())
	return nil
}

type leafNode struct {
	ID          string `json:"id"`
	Version     string `json:"version"`
	Provider    string `json:"provider,omitempty"`
	Project     string `json:"project,omitempty"`
	Filename    string `json:"filename,omitempty"`
	Environment string `json:"environment"`
	Ecosystem   string `json:"ecosystem"`
}

func outputLeavesJSON(
	graph *DependencyGraph,
	origin GraphOrigin,
	mode JSONRendering,
) error {
	leaves := graph.GetLeaves()
	jsonLeaves := make([]leafNode, 0, len(leaves))
	for _, leaf := range leaves {
		jsonLeaves = append(
			jsonLeaves, leafNode{
				ID:          leaf.ID,
				Version:     leaf.Version,
				Provider:    leaf.Provider.String(),
				Project:     leaf.ProjectID,
				Filename:    leaf.Filename,
				Environment: string(leaf.Environment),
				Ecosystem:   leaf.Ecosystem.String(),
			},
		)
	}

	output := map[string]any{
		"origin": origin.String(),
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
