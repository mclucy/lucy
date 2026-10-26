package cli

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/internal/cli/help"
	"github.com/mclucy/lucy/internal/fsutil"
	"github.com/mclucy/lucy/operation"
	"github.com/mclucy/lucy/server"
	"github.com/spf13/cobra"
)

var bisectCmd = &cobra.Command{
	Use:   "bisect",
	Short: help.BisectShort,
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var bisectStartCmd = &cobra.Command{
	Use:   "start",
	Short: help.BisectStartShort,
	Args:  cobra.NoArgs,
	RunE:  actionBisectStart,
}

var bisectGoodCmd = &cobra.Command{
	Use:   "good",
	Short: help.BisectGoodShort,
	Args:  cobra.NoArgs,
	RunE:  actionBisectGood,
}

var bisectBadCmd = &cobra.Command{
	Use:   "bad",
	Short: help.BisectBadShort,
	Args:  cobra.NoArgs,
	RunE:  actionBisectBad,
}

var bisectStatusCmd = &cobra.Command{
	Use:   "status",
	Short: help.BisectStatusShort,
	Args:  cobra.NoArgs,
	RunE:  actionBisectStatus,
}

var bisectResetCmd = &cobra.Command{
	Use:   "reset",
	Short: help.BisectResetShort,
	Args:  cobra.NoArgs,
	RunE:  actionBisectReset,
}

// bisectJSON is the shared machine-readable result of every bisect leaf: they
// all report progress through bisectOutput.
var bisectJSON JSONResult[bisectOutput]

func NewBisectCommand(contracts *JSONContracts) *cobra.Command {
	bisectJSON.Bind(contracts, bisectStartCmd)
	bisectJSON.Bind(contracts, bisectGoodCmd)
	bisectJSON.Bind(contracts, bisectBadCmd)
	bisectJSON.Bind(contracts, bisectStatusCmd)
	bisectJSON.Bind(contracts, bisectResetCmd)
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
// manifest's own "provider:project" key and Ecosystem is the ecosystem it is
// declared under; together they identify the entry unambiguously.
type bisectMod struct {
	Environment server.Environment `json:"environment"`
	Ecosystem   catalog.Ecosystem  `json:"ecosystem"`
	Reference   string             `json:"reference"`
	Version     string             `json:"version,omitempty"`
}

// bisectState is the persisted session. Original is the manifest as it stood
// when the session started, so reset restores it verbatim rather than trying
// to re-enable entries one by one.
type bisectState struct {
	Mods     []bisectMod        `json:"mods"`
	Original *document.Manifest `json:"original"`
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
	Warnings []string    `json:"warnings,omitempty"`
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
	if err := fsutil.AtomicWrite(bisectFilePath(workDir), data, 0o600); err != nil {
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
// the rest disabled. Every other manifest field is preserved. Sync facts come
// back as warnings so the command can fold them into its result instead of
// writing them past a JSON document.
func applyBisectRange(ctx context.Context, workDir string, mods []bisectMod, mid int) (enabled, disabled int, warnings []string, err error) {
	doc, err := document.ManifestFile(workDir).Read()
	if err != nil {
		return 0, 0, nil, err
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
		return 0, 0, nil, nil
	}
	if err := document.ManifestFile(workDir).Write(doc); err != nil {
		return 0, 0, nil, fmt.Errorf("write manifest: %w", err)
	}
	result, err := operation.Sync(ctx, workDir, install.Options{})
	if err != nil {
		return 0, 0, nil, fmt.Errorf("sync installation: %w", err)
	}
	return enabled, disabled, syncWarnings(result), nil
}

// setRequirementEnabled flips one manifest requirement and reports whether
// the entry was present.
func setRequirementEnabled(doc *document.Manifest, mod bisectMod, enabled bool) bool {
	var packages map[string]document.Requirement
	switch mod.Environment {
	case server.EnvironmentServer:
		if doc.Server.Packages == nil {
			return false
		}
		packages = doc.Server.Packages[mod.Ecosystem]
	case server.EnvironmentMCDR:
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
// and reports how many requirement entries that restored. As with applying a
// range, sync facts travel with the result.
func restoreBisectManifest(ctx context.Context, workDir string, original *document.Manifest) (restored int, warnings []string, err error) {
	if original == nil {
		return 0, nil, nil
	}
	if err := document.ManifestFile(workDir).Write(original); err != nil {
		return 0, nil, fmt.Errorf("restore manifest: %w", err)
	}
	result, err := operation.Sync(ctx, workDir, install.Options{})
	if err != nil {
		return 0, nil, fmt.Errorf("sync installation: %w", err)
	}
	return countRequirements(original), syncWarnings(result), nil
}

// syncWarnings collects the facts one sync produced so a bisect result can
// carry them. Diagnostics and warnings stay separate from stderr output, so a
// JSON run never emits a second document beside the result.
func syncWarnings(result *install.Result) []string {
	if result == nil {
		return nil
	}
	warnings := make([]string, 0, len(result.Diagnostics)+len(result.Warnings))
	for _, diagnostic := range result.Diagnostics {
		warnings = append(
			warnings,
			fmt.Sprintf("%s: %s", diagnostic.Path, diagnostic.Message),
		)
	}
	for _, warning := range result.Warnings {
		if warning != nil {
			warnings = append(warnings, warning.Error())
		}
	}
	return warnings
}

// countRequirements counts every declared requirement entry across the
// server and MCDR manifests, so the reported restore covers every runtime
// rather than one loader map.
func countRequirements(doc *document.Manifest) int {
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

func outputBisect(cmd *cobra.Command, view bisectOutput) error {
	output := NewOperationOutput(cmd)
	if cmd.Name() == "status" && cmd.Parent() != nil && cmd.Parent().Name() == "bisect" {
		output = NewQueryOutput(cmd)
	}
	if output.JSON() {
		return bisectJSON.Write(output, view)
	}
	if _, err := fmt.Fprintln(output.Human(), formatBisectOutput(view)); err != nil {
		return err
	}
	for _, warning := range view.Warnings {
		output.Warn(errors.New(warning))
	}
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
func bisectCandidates(workDir string, output *Output) ([]bisectMod, []string, error) {
	doc, err := document.ManifestFile(workDir).Read()
	if err != nil {
		return nil, nil, fmt.Errorf("read manifest: %w", err)
	}

	graph, _, _, err := LoadDependencyData(workDir, false, output)
	if err != nil {
		return nil, nil, err
	}

	ordered := graph.OrderedRoots()
	if ordered == nil {
		ordered = graph.GetRoots()
	}

	var mods []bisectMod
	seen := make(map[string]bool)
	for _, root := range ordered {
		if !requirementEnabled(
			doc,
			root.Environment,
			root.Ecosystem,
			root.ID,
		) {
			continue
		}
		mod := bisectMod{
			Environment: root.Environment,
			Ecosystem:   root.Ecosystem,
			Reference:   root.ID,
			Version:     root.Version,
		}
		key := string(mod.Environment) + "|" +
			string(mod.Ecosystem) + "|" + mod.Reference
		if seen[key] {
			continue
		}
		seen[key] = true
		mods = append(mods, mod)
	}
	return mods, output.Warnings(), nil
}

// requirementEnabled reports whether the manifest still enables the given
// requirement entry.
func requirementEnabled(
	doc *document.Manifest,
	environment server.Environment,
	ecosystem catalog.Ecosystem,
	reference string,
) bool {
	var packages map[string]document.Requirement
	switch environment {
	case server.EnvironmentServer:
		if doc.Server.Packages == nil {
			return false
		}
		packages = doc.Server.Packages[ecosystem]
	case server.EnvironmentMCDR:
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
	ctx, stop := CommandContext(cmd)
	defer stop()
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	candidateOutput := NewOperationOutput(cmd)
	mods, candidateWarnings, err := bisectCandidates(workDir, candidateOutput)
	if err != nil {
		return err
	}
	if len(mods) == 0 {
		return outputBisect(
			cmd,
			bisectOutput{
				Message:  "no enabled package requirements found in this manifest",
				Complete: true,
				Warnings: candidateWarnings,
			},
		)
	}

	original, err := document.ManifestFile(workDir).Read()
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
	enabled, disabled, warnings, err := applyBisectRange(ctx, workDir, mods, mid)
	if err != nil {
		return err
	}

	return outputBisect(
		cmd, bisectOutput{
			Message:  "session started",
			State:    currentBisectView(session),
			Enabled:  enabled,
			Disabled: disabled,
			Warnings: append(candidateWarnings, warnings...),
		},
	)
}

func actionBisectGood(cmd *cobra.Command, args []string) error {
	ctx, stop := CommandContext(cmd)
	defer stop()
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
		restored, warnings, err := restoreBisectManifest(ctx, workDir, session.Original)
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
				Warnings: warnings,
			},
		)
	}

	newMid := (session.L + session.R) / 2
	enabled, disabled, warnings, err := applyBisectRange(ctx, workDir, session.Mods, newMid)
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
			Warnings: warnings,
		},
	)
}

func actionBisectBad(cmd *cobra.Command, args []string) error {
	ctx, stop := CommandContext(cmd)
	defer stop()
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
		restored, warnings, err := restoreBisectManifest(ctx, workDir, session.Original)
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
				Warnings: warnings,
			},
		)
	}

	newMid := (session.L + session.R) / 2
	enabled, disabled, warnings, err := applyBisectRange(ctx, workDir, session.Mods, newMid)
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
			Warnings: warnings,
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

	summary := "session active"
	complete := false
	if session.L > session.R {
		summary = "session complete: no bad package found"
		complete = true
	} else if session.L == session.R {
		summary = "session complete: bad package identified"
		complete = true
	}

	return outputBisect(
		cmd,
		bisectOutput{
			Message:  summary,
			Complete: complete,
			State:    currentBisectView(session),
		},
	)
}

func actionBisectReset(cmd *cobra.Command, args []string) error {
	ctx, stop := CommandContext(cmd)
	defer stop()
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	session, err := readBisectState(workDir)
	if err != nil {
		return err
	}
	restored, warnings, err := restoreBisectManifest(ctx, workDir, session.Original)
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
			Warnings: warnings,
		},
	)
}
