package bisect

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/internal/cli"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/state"
	"github.com/mclucy/lucy/tui/style"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/workspace"
	"github.com/spf13/cobra"
)

var bisectCmd = &cobra.Command{
	Use:   "bisect",
	Short: "Find a problematic mod by binary search",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var bisectStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start a binary-search session",
	Args:  cobra.NoArgs,
	RunE:  cli.WithErrorLogging(actionBisectStart),
}

var bisectGoodCmd = &cobra.Command{
	Use:   "good",
	Short: "Mark current midpoint as good (bad mod is in right half)",
	Args:  cobra.NoArgs,
	RunE:  cli.WithErrorLogging(actionBisectGood),
}

var bisectBadCmd = &cobra.Command{
	Use:   "bad",
	Short: "Mark current midpoint as bad (bad mod is in left half)",
	Args:  cobra.NoArgs,
	RunE:  cli.WithErrorLogging(actionBisectBad),
}

var bisectStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the active bisect session",
	Args:  cobra.NoArgs,
	RunE:  cli.WithErrorLogging(actionBisectStatus),
}

var bisectResetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Abort the active bisect session and re-enable mods",
	Args:  cobra.NoArgs,
	RunE:  cli.WithErrorLogging(actionBisectReset),
}

// NewCommand wires and returns the `lucy bisect` command tree.
func NewCommand() *cobra.Command {
	for _, c := range []*cobra.Command{
		bisectStartCmd,
		bisectGoodCmd,
		bisectBadCmd,
		bisectStatusCmd,
		bisectResetCmd,
	} {
		cli.AddJSONFlag(c)
		cli.AddNoStyleFlag(c)
	}
	bisectCmd.AddCommand(
		bisectStartCmd,
		bisectGoodCmd,
		bisectBadCmd,
		bisectStatusCmd,
		bisectResetCmd,
	)
	return bisectCmd
}

type bisectMod struct {
	ID      types.PackageRef  `json:"id"`
	Version types.BareVersion `json:"version"`
	Path    string            `json:"path,omitempty"`
}

type bisectState struct {
	Mods []bisectMod `json:"mods"`
	L    int         `json:"l"`
	R    int         `json:"r"`
}

type bisectOutput struct {
	Message  string      `json:"message"`
	Complete bool        `json:"complete"`
	Found    *bisectMod  `json:"found,omitempty"`
	State    *bisectView `json:"state,omitempty"`
	Enabled  int         `json:"enabled"`
	Disabled int         `json:"disabled"`
	Restored int         `json:"restored"`
}

type bisectView struct {
	Total     int        `json:"total"`
	Left      int        `json:"left"`
	Right     int        `json:"right"`
	Midpoint  int        `json:"midpoint,omitempty"`
	Candidate *bisectMod `json:"candidate,omitempty"`
	Remaining int        `json:"remaining"`
}

func bisectFilePath(workDir string) string {
	return filepath.Join(workDir, "bisect.json")
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
	if err := validateBisectState(&state); err != nil {
		return nil, err
	}
	return &state, nil
}

func writeBisectState(workDir string, session *bisectState) error {
	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize bisect state: %w", err)
	}
	if err := state.AtomicWrite(
		bisectFilePath(workDir),
		data,
		0o600,
	); err != nil {
		return fmt.Errorf("failed to write bisect state: %w", err)
	}
	return nil
}

func deleteBisectState(workDir string) error {
	if err := os.Remove(bisectFilePath(workDir)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove bisect state: %w", err)
	}
	return nil
}

func validateBisectState(state *bisectState) error {
	if state == nil {
		return fmt.Errorf("invalid bisect state: empty state")
	}
	if len(state.Mods) == 0 {
		return fmt.Errorf("invalid bisect state: no mods")
	}
	if state.L < 0 || state.R >= len(state.Mods) {
		return fmt.Errorf(
			"invalid bisect state: range [%d, %d] outside %d mods",
			state.L,
			state.R,
			len(state.Mods),
		)
	}
	if state.L > state.R+1 {
		return fmt.Errorf(
			"invalid bisect state: range [%d, %d] is inconsistent",
			state.L,
			state.R,
		)
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

func applyBisectRange(mods []bisectMod, mid int) (
	enabled, disabled int,
	err error,
) {
	for i, m := range mods {
		if m.Path == "" {
			continue
		}
		if i <= mid {
			if err := enableMod(m.Path); err != nil {
				return enabled, disabled, fmt.Errorf(
					"enable %s: %w",
					m.Path,
					err,
				)
			}
			enabled++
		} else {
			if err := disableMod(m.Path); err != nil {
				return enabled, disabled, fmt.Errorf(
					"disable %s: %w",
					m.Path,
					err,
				)
			}
			disabled++
		}
	}
	return
}

func restoreBisectMods(mods []bisectMod) (int, error) {
	restored := 0
	for _, m := range mods {
		if m.Path == "" {
			continue
		}
		if err := enableMod(m.Path); err != nil {
			return restored, fmt.Errorf("enable %s: %w", m.Path, err)
		}
		restored++
	}
	return restored, nil
}

func currentBisectView(state *bisectState) *bisectView {
	view := &bisectView{
		Total:     len(state.Mods),
		Left:      state.L,
		Right:     state.R,
		Remaining: max(state.R-state.L+1, 0),
	}
	if state.L <= state.R {
		mid := (state.L + state.R) / 2
		view.Midpoint = mid
		view.Candidate = &state.Mods[mid]
	}
	return view
}

func outputBisect(cmd *cobra.Command, output bisectOutput) error {
	jsonOut, _ := cmd.Flags().GetBool(cli.FlagJSON)
	jsonCompact, _ := cmd.Flags().GetBool(cli.FlagJSONCompact)
	if jsonOut || jsonCompact {
		if jsonCompact {
			style.PrintAsJsonCompact(output)
		} else {
			style.PrintAsJson(output)
		}
		return nil
	}
	log.ShowInfo(formatBisectOutput(output))
	return nil
}

func formatBisectOutput(output bisectOutput) string {
	var builder strings.Builder
	builder.WriteString("Bisect: ")
	builder.WriteString(output.Message)
	builder.WriteByte('\n')
	if output.State != nil {
		builder.WriteString(
			fmt.Sprintf(
				"Range: [%d, %d]\n",
				output.State.Left,
				output.State.Right,
			),
		)
		builder.WriteString(
			fmt.Sprintf(
				"Remaining: %d of %d\n",
				output.State.Remaining,
				output.State.Total,
			),
		)
		if output.State.Candidate != nil {
			builder.WriteString(
				fmt.Sprintf(
					"Candidate: %s (midpoint %d)\n",
					bisectModLabel(*output.State.Candidate),
					output.State.Midpoint,
				),
			)
		}
	}
	if output.Found != nil {
		builder.WriteString("Bad mod: ")
		builder.WriteString(bisectModLabel(*output.Found))
		builder.WriteByte('\n')
		if output.Found.Path != "" {
			builder.WriteString("File: ")
			builder.WriteString(output.Found.Path)
			builder.WriteByte('\n')
		}
	}
	if output.Enabled > 0 || output.Disabled > 0 || output.Restored > 0 {
		builder.WriteString(
			fmt.Sprintf(
				"Changed: enabled %d, disabled %d, restored %d\n",
				output.Enabled,
				output.Disabled,
				output.Restored,
			),
		)
	}
	if !output.Complete && output.State != nil && output.State.Candidate != nil {
		builder.WriteString("Test your server, then run `lucy bisect good` or `lucy bisect bad`.")
	}
	return strings.TrimRight(builder.String(), "\n")
}

func bisectModLabel(mod bisectMod) string {
	return mod.ID.StringBase() + "@" + mod.Version.String()
}

func actionBisectStart(cmd *cobra.Command, args []string) error {
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	info := workspace.NewAt(workDir)
	if len(info.Packages) == 0 {
		return outputBisect(
			cmd,
			bisectOutput{
				Message:  "no mods found in this server directory",
				Complete: true,
			},
		)
	}

	graph, _, err := cli.LoadDependencyData(workDir, true)
	if err != nil {
		return err
	}

	sorted := graph.TopologicalSort()
	message := "session started"
	if sorted == nil {
		message = "session started with dependency cycle; using alphabetical order"
		sorted = make([]*cli.GraphNode, 0, len(graph.Nodes))
		for _, node := range graph.Nodes {
			sorted = append(sorted, node)
		}
		sort.Slice(
			sorted, func(i, j int) bool {
				return sorted[i].ID < sorted[j].ID
			},
		)
	}

	pathByID := make(map[string]string, len(info.Packages))
	for _, p := range info.Packages {
		if p.Path != "" {
			pathByID[p.Id.StringBase()] = p.Path
		}
	}

	mods := make([]bisectMod, 0, len(sorted))
	for _, node := range sorted {
		ref, _, err := input.Parse(node.ID)
		if err != nil {
			continue
		}
		if types.IsCorePackage(ref.PackageRef) {
			continue
		}
		mods = append(
			mods, bisectMod{
				ID:      ref.PackageRef,
				Version: types.BareVersion(node.Version),
				Path:    pathByID[node.ID],
			},
		)
	}

	if len(mods) == 0 {
		return outputBisect(
			cmd,
			bisectOutput{
				Message:  "no mods found after filtering core packages",
				Complete: true,
			},
		)
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
	enabled, disabled, err := applyBisectRange(mods, mid)
	if err != nil {
		return err
	}

	return outputBisect(
		cmd, bisectOutput{
			Message:  message,
			State:    currentBisectView(state),
			Enabled:  enabled,
			Disabled: disabled,
		},
	)
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
		return outputBisect(
			cmd,
			bisectOutput{
				Message: "session complete: no bad mod found", Complete: true,
				State: currentBisectView(state),
			},
		)
	}

	mid := (state.L + state.R) / 2
	state.L = mid + 1
	if state.L > state.R {
		if err := writeBisectState(workDir, state); err != nil {
			return err
		}
		return outputBisect(
			cmd,
			bisectOutput{
				Message:  "all remaining mods are good; no bad mod found",
				Complete: true, State: currentBisectView(state),
			},
		)
	}

	newMid := (state.L + state.R) / 2
	enabled, disabled, err := applyBisectRange(state.Mods, newMid)
	if err != nil {
		return err
	}
	if err := writeBisectState(workDir, state); err != nil {
		return err
	}

	return outputBisect(
		cmd, bisectOutput{
			Message: fmt.Sprintf(
				"marked %s good",
				bisectModLabel(state.Mods[mid]),
			),
			State:    currentBisectView(state),
			Enabled:  enabled,
			Disabled: disabled,
		},
	)
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
	if state.L > state.R {
		return outputBisect(
			cmd,
			bisectOutput{
				Message: "session complete: no bad mod found", Complete: true,
				State: currentBisectView(state),
			},
		)
	}

	mid := (state.L + state.R) / 2
	state.R = mid
	if state.L == state.R {
		var restored int
		for i, m := range state.Mods {
			if m.Path == "" {
				continue
			}
			if i == state.L {
				if err := disableMod(m.Path); err != nil {
					return err
				}
			} else {
				if err := enableMod(m.Path); err != nil {
					return err
				}
				restored++
			}
		}
		if err := writeBisectState(workDir, state); err != nil {
			return err
		}
		return outputBisect(
			cmd, bisectOutput{
				Message:  "found bad mod",
				Complete: true,
				Found:    new(state.Mods[state.L]),
				State:    currentBisectView(state),
				Disabled: 1,
				Restored: restored,
			},
		)
	}

	newMid := (state.L + state.R) / 2
	enabled, disabled, err := applyBisectRange(state.Mods, newMid)
	if err != nil {
		return err
	}
	if err := writeBisectState(workDir, state); err != nil {
		return err
	}

	return outputBisect(
		cmd, bisectOutput{
			Message: fmt.Sprintf(
				"marked %s bad",
				bisectModLabel(state.Mods[mid]),
			),
			State:    currentBisectView(state),
			Enabled:  enabled,
			Disabled: disabled,
		},
	)
}

func actionBisectStatus(cmd *cobra.Command, args []string) error {
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	state, err := readBisectState(workDir)
	if err != nil {
		return err
	}

	message := "session active"
	complete := false
	if state.L > state.R {
		message = "session complete: no bad mod found"
		complete = true
	} else if state.L == state.R {
		message = "session complete: bad mod identified"
		complete = true
	}

	return outputBisect(
		cmd, bisectOutput{
			Message:  message,
			Complete: complete,
			State:    currentBisectView(state),
		},
	)
}

func actionBisectReset(cmd *cobra.Command, args []string) error {
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	state, err := readBisectState(workDir)
	if err != nil {
		return err
	}
	restored, err := restoreBisectMods(state.Mods)
	if err != nil {
		return err
	}
	if err := deleteBisectState(workDir); err != nil {
		return err
	}

	return outputBisect(
		cmd, bisectOutput{
			Message:  "session reset",
			Complete: true,
			Restored: restored,
		},
	)
}
