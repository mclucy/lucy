package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/mclucy/lucy/probe"
	"github.com/mclucy/lucy/state"
	"github.com/spf13/cobra"
)

var debugCmd = &cobra.Command{
	Use:   "debug",
	Short: "Use algorithm find mods have bug smartly",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var debugStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start a binary-search debug session",
	Args:  cobra.NoArgs,
	RunE:  runWithErrorLogging(actionDebugStart),
}

var debugGoodCmd = &cobra.Command{
	Use:   "good",
	Short: "Mark current midpoint as good (bad mod is in right half)",
	Args:  cobra.NoArgs,
	RunE:  runWithErrorLogging(actionDebugGood),
}

var debugBadCmd = &cobra.Command{
	Use:   "bad",
	Short: "Mark current midpoint as bad (bad mod is in left half)",
	Args:  cobra.NoArgs,
	RunE:  runWithErrorLogging(actionDebugBad),
}

func init() {
	debugCmd.AddCommand(debugStartCmd, debugGoodCmd, debugBadCmd)
	rootCmd.AddCommand(debugCmd)
}

type debugMod struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

type debugState struct {
	Mods []debugMod `json:"mods"`
	L    int        `json:"l"`
	R    int        `json:"r"`
}

func debugFilePath(workDir string) string {
	return filepath.Join(workDir, ".lucy", "debug.json")
}

func readDebugState(workDir string) (*debugState, error) {
	data, err := os.ReadFile(debugFilePath(workDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no debug session found, run `lucy debug start` first")
		}
		return nil, fmt.Errorf("failed to read debug state: %w", err)
	}
	var state debugState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to parse debug state: %w", err)
	}
	return &state, nil
}

func writeDebugState(workDir string, state *debugState) error {
	lucyDir := filepath.Join(workDir, ".lucy")
	if err := os.MkdirAll(lucyDir, 0o755); err != nil {
		return fmt.Errorf("failed to create .lucy directory: %w", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize debug state: %w", err)
	}
	if err := os.WriteFile(debugFilePath(workDir), data, 0o600); err != nil {
		return fmt.Errorf("failed to write debug state: %w", err)
	}
	return nil
}

func actionDebugStart(cmd *cobra.Command, args []string) error {
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	info := probe.ServerInfo()
	if len(info.Packages) == 0 {
		fmt.Println("No mods found in this server directory")
		return nil
	}

	cfg, _, err := state.ReadConfig(workDir)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	identitySet := make(map[string]bool, len(cfg.Debug.IdentityPackages))
	for _, id := range cfg.Debug.IdentityPackages {
		identitySet[id] = true
	}

	graph, err := BuildGraphFromProbe(info)
	if err != nil {
		return fmt.Errorf("failed to build dependency graph: %w", err)
	}

	sorted := graph.TopologicalSort()
	if sorted == nil {
		fmt.Println("Warning: dependency cycle detected, using alphabetical order")
		sorted = make([]*GraphNode, 0, len(graph.Nodes))
		for _, node := range graph.Nodes {
			sorted = append(sorted, node)
		}
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].ID < sorted[j].ID
		})
	}

	mods := make([]debugMod, 0, len(sorted))
	for _, node := range sorted {
		if !identitySet[node.ID] {
			mods = append(mods, debugMod{ID: node.ID, Version: node.Version})
		}
	}

	if len(mods) == 0 {
		fmt.Println("No mods found after filtering identity packages")
		return nil
	}

	state := &debugState{
		Mods: mods,
		L:    0,
		R:    len(mods) - 1,
	}
	if err := writeDebugState(workDir, state); err != nil {
		return err
	}

	mid := (state.L + state.R) / 2
	fmt.Printf("Debug session started\n")
	fmt.Printf("Mods: %d (topologically sorted)\n", len(mods))
	fmt.Printf("Range: [%d, %d]\n", state.L, state.R)
	fmt.Printf("Midpoint: %d (%s@%s)\n", mid, mods[mid].ID, mods[mid].Version)
	fmt.Printf("\nTest with all mods up to index %d enabled, then run `lucy debug good` or `lucy debug bad`\n", mid)
	return nil
}

func actionDebugGood(cmd *cobra.Command, args []string) error {
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	state, err := readDebugState(workDir)
	if err != nil {
		return err
	}

	if state.L > state.R {
		fmt.Println("Debug session complete: no bad mod found")
		fmt.Println("Run `lucy debug start` to begin a new debug session")
		return nil
	}

	mid := (state.L + state.R) / 2
	fmt.Printf("Midpoint %d (%s@%s) is GOOD\n", mid, state.Mods[mid].ID, state.Mods[mid].Version)

	state.L = mid + 1
	if state.L > state.R {
		fmt.Println("All remaining mods are good. No bad mod found.")
		fmt.Println("Run `lucy debug start` to begin a new debug session")
		_ = writeDebugState(workDir, state)
		return nil
	}

	newMid := (state.L + state.R) / 2
	fmt.Printf("New range: [%d, %d]\n", state.L, state.R)
	fmt.Printf("Next midpoint: %d (%s@%s)\n", newMid, state.Mods[newMid].ID, state.Mods[newMid].Version)

	return writeDebugState(workDir, state)
}

func actionDebugBad(cmd *cobra.Command, args []string) error {
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	state, err := readDebugState(workDir)
	if err != nil {
		return err
	}

	mid := (state.L + state.R) / 2
	fmt.Printf("Midpoint %d (%s@%s) is BAD\n", mid, state.Mods[mid].ID, state.Mods[mid].Version)

	state.R = mid
	if state.L == state.R {
		badMod := state.Mods[state.L]
		fmt.Printf("\nFound bad mod: %s@%s\n", badMod.ID, badMod.Version)
		fmt.Println("Run `lucy debug start` to begin a new debug session")
		_ = writeDebugState(workDir, state)
		return nil
	}

	newMid := (state.L + state.R) / 2
	fmt.Printf("New range: [%d, %d]\n", state.L, state.R)
	fmt.Printf("Next midpoint: %d (%s@%s)\n", newMid, state.Mods[newMid].ID, state.Mods[newMid].Version)

	return writeDebugState(workDir, state)
}
