package cli

import (
	"fmt"
	"os"

	"github.com/mclucy/lucy/internal/cli/help"

	"github.com/spf13/cobra"
)

var leavesCmd = &cobra.Command{
	Use:   "leaves",
	Short: help.LeavesShort,
	Args:  cobra.NoArgs,
	RunE:  actionLeaves,
}

var leavesJSON JSONResult[leavesOutput]

func NewLeavesCommand(contracts *JSONContracts) *cobra.Command {
	leavesJSON.Bind(contracts, leavesCmd)
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
	output := NewQueryOutput(cmd)
	graph, origin, _, err := LoadDependencyData(workDir, forceLive, output)
	if err != nil {
		return err
	}

	if output.JSON() {
		return leavesJSON.Write(output, leavesDocument(graph, origin, output.Warnings()))
	}

	human := output.Human()
	fmt.Fprintf(human, "Using data from: %s\n\n", origin.String())

	leaves := graph.GetLeaves()
	if len(leaves) == 0 {
		fmt.Fprintln(human, "No leaf packages found")
	} else {
		for _, leaf := range leaves {
			fmt.Fprintln(human, nodeLabel(leaf))
		}
	}

	fmt.Fprintf(human, "\n(from %s)\n", origin.String())
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

// leavesOutput is the complete machine-readable `leaves` result.
type leavesOutput struct {
	Leaves   []leafNode `json:"leaves"`
	Origin   string     `json:"origin"`
	Warnings []string   `json:"warnings,omitempty"`
}

func leavesDocument(
	graph *DependencyGraph,
	origin GraphOrigin,
	warnings []string,
) leavesOutput {
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

	return leavesOutput{
		Leaves:   jsonLeaves,
		Origin:   origin.String(),
		Warnings: warnings,
	}
}
