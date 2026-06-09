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

var bisectCmd = &cobra.Command{
	Use:   "bisect",
	Short: "Use algorithm find mods have bug smartly",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var bisectStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start a binary-search session",
	Args:  cobra.NoArgs,
	RunE:  runWithErrorLogging(actionBisectStart),
}

var bisectGoodCmd = &cobra.Command{
	Use:   "good",
	Short: "Mark current midpoint as good (bad mod is in right half)",
	Args:  cobra.NoArgs,
	RunE:  runWithErrorLogging(actionBisectGood),
}

var bisectBadCmd = &cobra.Command{
	Use:   "bad",
	Short: "Mark current midpoint as bad (bad mod is in left half)",
	Args:  cobra.NoArgs,
	RunE:  runWithErrorLogging(actionBisectBad),
}

func init() {
	bisectCmd.AddCommand(bisectStartCmd, bisectGoodCmd, bisectBadCmd)
	rootCmd.AddCommand(bisectCmd)
}

type bisectMod struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Path    string `json:"path"`
}

type bisectState struct {
	Mods []bisectMod `json:"mods"`
	L    int         `json:"l"`
	R    int         `json:"r"`
}

func bisectFilePath(workDir string) string {
	return filepath.Join(workDir, ".lucy", "bisect.json")
}

func readBisectState(workDir string) (*bisectState, error) {
	data, err := os.ReadFile(bisectFilePath(workDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no bisect session found, run `lucy bisect start` first")
		}
		return nil, fmt.Errorf("failed to read bisect state: %w", err)
	}
	var state bisectState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to parse bisect state: %w", err)
	}
	return &state, nil
}

func writeBisectState(workDir string, state *bisectState) error {
	lucyDir := filepath.Join(workDir, ".lucy")
	if err := os.MkdirAll(lucyDir, 0o755); err != nil {
		return fmt.Errorf("failed to create .lucy directory: %w", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize bisect state: %w", err)
	}
	if err := os.WriteFile(bisectFilePath(workDir), data, 0o600); err != nil {
		return fmt.Errorf("failed to write bisect state: %w", err)
	}
	return nil
}

func enableMod(path string) error {
	dp := path + ".disabled"
	if _, err := os.Stat(dp); os.IsNotExist(err) {
		return nil
	}
	_ = os.Remove(path)
	return os.Rename(dp, path)
}

func disableMod(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	dp := path + ".disabled"
	_ = os.Remove(dp)
	return os.Rename(path, dp)
}

func applyBisectRange(mods []bisectMod, mid int) (enabled, disabled int) {
	for i, m := range mods {
		if m.Path == "" {
			continue
		}
		if i <= mid {
			if err := enableMod(m.Path); err == nil {
				enabled++
			}
		} else {
			if err := disableMod(m.Path); err == nil {
				disabled++
			}
		}
	}
	return
}

func printRange(state *bisectState, mid int, enabled, disabled int) {
	if enabled > 0 {
		fmt.Printf("Enabled %d mods in range [0, %d]\n", enabled, mid)
	}
	if disabled > 0 {
		fmt.Printf("Disabled %d mods in range [%d, %d]\n", disabled, mid+1, state.R)
	}
	fmt.Printf("Test your server, then run `lucy bisect good` or `lucy bisect bad`\n")
}

func actionBisectStart(cmd *cobra.Command, args []string) error {
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

	identitySet := make(map[string]bool, len(cfg.Bisect.IdentityPackages))
	for _, id := range cfg.Bisect.IdentityPackages {
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

	pathByID := make(map[string]string, len(info.Packages))
	for _, p := range info.Packages {
		key := string(p.Id.Platform) + "/" + string(p.Id.Name)
		if p.Local != nil && p.Local.Path != "" {
			pathByID[key] = p.Local.Path
		}
	}

	mods := make([]bisectMod, 0, len(sorted))
	for _, node := range sorted {
		if identitySet[node.ID] {
			continue
		}
		mods = append(mods, bisectMod{
			ID:      node.ID,
			Version: node.Version,
			Path:    pathByID[node.ID],
		})
	}

	if len(mods) == 0 {
		fmt.Println("No mods found after filtering identity packages")
		return nil
	}

	state := &bisectState{
		Mods: mods,
		L:    0,
		R:    len(mods) - 1,
	}
	if err := writeBisectState(workDir, state); err != nil {
		return err
	}

	mid := (state.L + state.R) / 2
	enabled, disabled := applyBisectRange(mods, mid)

	fmt.Printf("Bisect session started\n")
	fmt.Printf("Mods: %d (topologically sorted)\n", len(mods))
	printRange(state, mid, enabled, disabled)
	return nil
}

func actionBisectGood(cmd *cobra.Command, args []string) error {
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	state, err := readBisectState(workDir)
	if err != nil {
		return err
	}

	if state.L > state.R {
		fmt.Println("Bisect session complete: no bad mod found")
		fmt.Println("Run `lucy bisect start` to begin a new bisect session")
		return nil
	}

	mid := (state.L + state.R) / 2
	fmt.Printf("Midpoint %d (%s@%s) is GOOD\n", mid, state.Mods[mid].ID, state.Mods[mid].Version)

	state.L = mid + 1
	if state.L > state.R {
		fmt.Println("All remaining mods are good. No bad mod found.")
		fmt.Println("Run `lucy bisect start` to begin a new bisect session")
		_ = writeBisectState(workDir, state)
		return nil
	}

	newMid := (state.L + state.R) / 2
	enabled, disabled := applyBisectRange(state.Mods, newMid)
	fmt.Printf("New range: [%d, %d]\n", state.L, state.R)
	printRange(state, newMid, enabled, disabled)

	return writeBisectState(workDir, state)
}

func actionBisectBad(cmd *cobra.Command, args []string) error {
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	state, err := readBisectState(workDir)
	if err != nil {
		return err
	}

	mid := (state.L + state.R) / 2
	fmt.Printf("Midpoint %d (%s@%s) is BAD\n", mid, state.Mods[mid].ID, state.Mods[mid].Version)

	state.R = mid
	if state.L == state.R {
		badMod := state.Mods[state.L]
		// Restore all mods except the bad one
		var restored int
		for i, m := range state.Mods {
			if m.Path == "" {
				continue
			}
			if i == state.L {
				_ = disableMod(m.Path)
			} else {
				if err := enableMod(m.Path); err == nil {
					restored++
				}
			}
		}
		_ = writeBisectState(workDir, state)
		fmt.Printf("\nFound bad mod: %s@%s\n", badMod.ID, badMod.Version)
		if badMod.Path != "" {
			fmt.Printf("File: %s\n", badMod.Path)
		}
		fmt.Printf("Disabled 1 mod (the bad one), enabled all other %d mods\n", restored)
		fmt.Println("Run `lucy bisect start` to begin a new bisect session")
		return nil
	}

	newMid := (state.L + state.R) / 2
	enabled, disabled := applyBisectRange(state.Mods, newMid)
	fmt.Printf("New range: [%d, %d]\n", state.L, state.R)
	printRange(state, newMid, enabled, disabled)

	return writeBisectState(workDir, state)
}
