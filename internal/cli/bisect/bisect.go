package bisect

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/internal/cli"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/manifest"
	"github.com/mclucy/lucy/terminal/style"
	"github.com/mclucy/lucy/types"
	"github.com/spf13/cobra"
)

var bisectCmd = &cobra.Command{
	Use:   "bisect",
	Short: "Find a problematic package by binary search",
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
	Short: "Mark current midpoint as good (bad package is in right half)",
	Args:  cobra.NoArgs,
	RunE:  cli.WithErrorLogging(actionBisectGood),
}

var bisectBadCmd = &cobra.Command{
	Use:   "bad",
	Short: "Mark current midpoint as bad (bad package is in the left half)",
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
	Short: "Abort the active bisect session and restore the manifest",
	Args:  cobra.NoArgs,
	RunE:  cli.WithErrorLogging(actionBisectReset),
}

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

// bisectMod is one manifest requirement the session toggles. Reference is the
// manifest's own "provider:project" key and Loader is the ecosystem it is
// declared under; together they identify the entry unambiguously.
type bisectMod struct {
	Runtime   string          `json:"runtime"`
	Loader    types.Ecosystem `json:"loader"`
	Reference string          `json:"reference"`
	Version   string          `json:"version,omitempty"`
}

// bisectState is the persisted session. Original is the manifest as it stood
// when the session started, so reset restores it verbatim rather than trying
// to re-enable entries one by one.
type bisectState struct {
	Mods     []bisectMod        `json:"mods"`
	Original *manifest.Document `json:"original"`
	L        int                `json:"l"`
	R        int                `json:"r"`
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
	var session bisectState
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("failed to parse bisect state: %w", err)
	}
	if err := validateBisectState(&session); err != nil {
		return nil, err
	}
	return &session, nil
}

func writeBisectState(workDir string, session *bisectState) error {
	data, err := json.Marshal(session, jsontext.Multiline(true), jsontext.WithIndent("  "))
	if err != nil {
		return fmt.Errorf("failed to serialize bisect state: %w", err)
	}
	if err := manifest.AtomicWrite(bisectFilePath(workDir), data); err != nil {
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

func validateBisectState(session *bisectState) error {
	if session == nil {
		return fmt.Errorf("invalid bisect state: empty state")
	}
	if len(session.Mods) == 0 {
		return fmt.Errorf("invalid bisect state: no packages")
	}
	if session.Original == nil {
		return fmt.Errorf("invalid bisect state: no captured manifest")
	}
	if session.L < 0 || session.R >= len(session.Mods) {
		return fmt.Errorf(
			"invalid bisect state: range [%d, %d] outside %d packages",
			session.L,
			session.R,
			len(session.Mods),
		)
	}
	if session.L > session.R+1 {
		return fmt.Errorf(
			"invalid bisect state: range [%d, %d] is inconsistent",
			session.L,
			session.R,
		)
	}
	return nil
}

// applyBisectRange writes the manifest with entries up to mid enabled and
// the rest disabled. Every other manifest field is preserved.
func applyBisectRange(workDir string, mods []bisectMod, mid int) (enabled, disabled int, err error) {
	doc, err := manifest.Read(workDir)
	if err != nil {
		return 0, 0, err
	}
	changed := 0
	for i, mod := range mods {
		if setRequirementEnabled(doc, mod, i <= mid) {
			changed++
			if i <= mid {
				enabled++
			} else {
				disabled++
			}
		}
	}
	if changed == 0 {
		return 0, 0, nil
	}
	if err := manifest.Write(workDir, doc); err != nil {
		return 0, 0, fmt.Errorf("write manifest: %w", err)
	}
	if err := install.Run(context.Background(), workDir, install.Options{}); err != nil {
		return 0, 0, fmt.Errorf("sync installation: %w", err)
	}
	return enabled, disabled, nil
}

// setRequirementEnabled flips one manifest requirement and reports whether
// the entry was present.
func setRequirementEnabled(doc *manifest.Document, mod bisectMod, enabled bool) bool {
	var packages map[string]manifest.Requirement
	switch mod.Runtime {
	case cli.RuntimeServer:
		if doc.Server.Packages == nil {
			return false
		}
		packages = doc.Server.Packages[mod.Loader]
	case cli.RuntimeMCDR:
		if doc.MCDR == nil {
			return false
		}
		packages = doc.MCDR.Packages
	default:
		return false
	}
	requirement, ok := packages[mod.Reference]
	if !ok {
		return false
	}
	value := enabled
	requirement.Enabled = &value
	packages[mod.Reference] = requirement
	return true
}

// restoreBisectManifest writes back the manifest captured at session start
// and reports how many requirement entries that restored.
func restoreBisectManifest(workDir string, original *manifest.Document) (int, error) {
	if original == nil {
		return 0, nil
	}
	if err := manifest.Write(workDir, original); err != nil {
		return 0, fmt.Errorf("restore manifest: %w", err)
	}
	if err := install.Run(context.Background(), workDir, install.Options{}); err != nil {
		return 0, fmt.Errorf("sync installation: %w", err)
	}
	return countRequirements(original), nil
}

// countRequirements counts every declared requirement entry across the
// server and MCDR manifests, so the reported restore covers every runtime
// rather than one loader map.
func countRequirements(doc *manifest.Document) int {
	total := 0
	for _, packages := range doc.Server.Packages {
		total += len(packages)
	}
	if doc.MCDR != nil {
		total += len(doc.MCDR.Packages)
	}
	return total
}

func currentBisectView(session *bisectState) *bisectView {
	view := &bisectView{
		Total:     len(session.Mods),
		Left:      session.L,
		Right:     session.R,
		Remaining: max(session.R-session.L+1, 0),
	}
	if session.L <= session.R {
		mid := (session.L + session.R) / 2
		view.Midpoint = mid
		view.Candidate = &session.Mods[mid]
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
		builder.WriteString("Bad package: ")
		builder.WriteString(bisectModLabel(*output.Found))
		builder.WriteByte('\n')
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
		builder.WriteString("Reinstall your packages, test the server, then run `lucy bisect good` or `lucy bisect bad`.")
	}
	return strings.TrimRight(builder.String(), "\n")
}

func bisectModLabel(mod bisectMod) string {
	if mod.Version == "" {
		return mod.Reference
	}
	return mod.Reference + "@" + mod.Version
}

// bisectCandidates returns the manifest requirements a session may toggle,
// ordered so a package always follows the packages it depends on.
func bisectCandidates(workDir string) ([]bisectMod, error) {
	doc, err := manifest.Read(workDir)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	graph, _, _, err := cli.LoadDependencyData(workDir, false)
	if err != nil {
		return nil, err
	}

	ordered := graph.OrderedRoots()
	if ordered == nil {
		ordered = graph.GetRoots()
	}

	var mods []bisectMod
	seen := make(map[string]bool)
	for _, root := range ordered {
		if !requirementEnabled(doc, root.Runtime, root.Loader, root.ID) {
			continue
		}
		mod := bisectMod{
			Runtime:   root.Runtime,
			Loader:    root.Loader,
			Reference: root.ID,
			Version:   root.Version,
		}
		key := mod.Runtime + "|" + string(mod.Loader) + "|" + mod.Reference
		if seen[key] {
			continue
		}
		seen[key] = true
		mods = append(mods, mod)
	}
	return mods, nil
}

// requirementEnabled reports whether the manifest still enables the given
// requirement entry.
func requirementEnabled(
	doc *manifest.Document,
	runtime string,
	loader types.Ecosystem,
	reference string,
) bool {
	var packages map[string]manifest.Requirement
	switch runtime {
	case cli.RuntimeServer:
		if doc.Server.Packages == nil {
			return false
		}
		packages = doc.Server.Packages[loader]
	case cli.RuntimeMCDR:
		if doc.MCDR == nil {
			return false
		}
		packages = doc.MCDR.Packages
	default:
		return false
	}
	requirement, ok := packages[reference]
	return ok && requirement.IsEnabled()
}

func actionBisectStart(cmd *cobra.Command, args []string) error {
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	mods, err := bisectCandidates(workDir)
	if err != nil {
		return err
	}
	if len(mods) == 0 {
		return outputBisect(
			cmd,
			bisectOutput{
				Message:  "no enabled package requirements found in this manifest",
				Complete: true,
			},
		)
	}

	original, err := manifest.Read(workDir)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}

	session := &bisectState{
		Mods:     mods,
		Original: original,
		L:        0,
		R:        len(mods) - 1,
	}
	if err := writeBisectState(workDir, session); err != nil {
		return err
	}

	mid := (session.L + session.R) / 2
	enabled, disabled, err := applyBisectRange(workDir, mods, mid)
	if err != nil {
		return err
	}

	return outputBisect(
		cmd, bisectOutput{
			Message:  "session started",
			State:    currentBisectView(session),
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

	session, err := readBisectState(workDir)
	if err != nil {
		return err
	}

	if session.L > session.R {
		return outputBisect(
			cmd,
			bisectOutput{
				Message:  "session complete: no bad package found",
				Complete: true,
				State:    currentBisectView(session),
			},
		)
	}

	mid := (session.L + session.R) / 2
	session.L = mid + 1
	if session.L > session.R {
		restored, err := restoreBisectManifest(workDir, session.Original)
		if err != nil {
			return err
		}
		if err := writeBisectState(workDir, session); err != nil {
			return err
		}
		return outputBisect(
			cmd,
			bisectOutput{
				Message:  "all remaining packages are good; no bad package found",
				Complete: true,
				State:    currentBisectView(session),
				Restored: restored,
			},
		)
	}

	newMid := (session.L + session.R) / 2
	enabled, disabled, err := applyBisectRange(workDir, session.Mods, newMid)
	if err != nil {
		return err
	}
	if err := writeBisectState(workDir, session); err != nil {
		return err
	}

	return outputBisect(
		cmd, bisectOutput{
			Message: fmt.Sprintf(
				"marked %s good",
				bisectModLabel(session.Mods[mid]),
			),
			State:    currentBisectView(session),
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

	session, err := readBisectState(workDir)
	if err != nil {
		return err
	}
	if session.L > session.R {
		return outputBisect(
			cmd,
			bisectOutput{
				Message:  "session complete: no bad package found",
				Complete: true,
				State:    currentBisectView(session),
			},
		)
	}

	mid := (session.L + session.R) / 2
	session.R = mid
	if session.L == session.R {
		restored, err := restoreBisectManifest(workDir, session.Original)
		if err != nil {
			return err
		}
		if err := writeBisectState(workDir, session); err != nil {
			return err
		}
		return outputBisect(
			cmd,
			bisectOutput{
				Message:  "found bad package",
				Complete: true,
				Found:    new(session.Mods[session.L]),
				State:    currentBisectView(session),
				Restored: restored,
			},
		)
	}

	newMid := (session.L + session.R) / 2
	enabled, disabled, err := applyBisectRange(workDir, session.Mods, newMid)
	if err != nil {
		return err
	}
	if err := writeBisectState(workDir, session); err != nil {
		return err
	}

	return outputBisect(
		cmd, bisectOutput{
			Message: fmt.Sprintf(
				"marked %s bad",
				bisectModLabel(session.Mods[mid]),
			),
			State:    currentBisectView(session),
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

	session, err := readBisectState(workDir)
	if err != nil {
		return err
	}

	message := "session active"
	complete := false
	if session.L > session.R {
		message = "session complete: no bad package found"
		complete = true
	} else if session.L == session.R {
		message = "session complete: bad package identified"
		complete = true
	}

	return outputBisect(
		cmd,
		bisectOutput{
			Message:  message,
			Complete: complete,
			State:    currentBisectView(session),
		},
	)
}

func actionBisectReset(cmd *cobra.Command, args []string) error {
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	session, err := readBisectState(workDir)
	if err != nil {
		return err
	}
	restored, err := restoreBisectManifest(workDir, session.Original)
	if err != nil {
		return err
	}
	if err := deleteBisectState(workDir); err != nil {
		return err
	}

	return outputBisect(
		cmd,
		bisectOutput{
			Message:  "session reset",
			Complete: true,
			Restored: restored,
		},
	)
}
