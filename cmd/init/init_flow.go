// Package init defines the UX contract and state machine for the lucy init
// command. It covers the interactive multi-step flow, the non-interactive
// (--yes) fast path, and conflict-resolution semantics for partial .lucy/
// directories.
//
// This file intentionally contains NO huh/bubbletea TUI code. The flow logic is pure and testable
// without a terminal.
package init

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/state"
)

// Constants and types related to the init flow state machine, result construction,

// InitStep names a discrete stage in the init flow.
type InitStep string

const (
	// StepWelcome is the opening screen. It explains what lucy init does and
	// what files it will create. No input is collected here.
	StepWelcome InitStep = "welcome"

	// StepGameVersion asks the user for the Minecraft game version (e.g.
	// "1.21.4"). This is the only mandatory question for a minimal init.
	StepGameVersion InitStep = "game_version"

	// StepPlatform asks which server platform to use.
	// Valid values: "fabric", "neoforge", "forge", "mcdr", "none"
	// "none" means vanilla or an as-yet-unknown platform.
	StepPlatform InitStep = "platform"

	// StepPlatformVersion asks for the platform loader version. This step is
	// skipped when Platform == "none".
	StepPlatformVersion InitStep = "platform_version"

	// StepSources lets the user configure source priority (modrinth, curseforge,
	// github, mcdr). This step is optional and may be skipped in minimal flows.
	StepSources InitStep = "sources"

	// StepManagedScope lets the user confirm or modify which root directories
	// Lucy should manage (e.g. mods/, plugins/, config/). The default list is
	// taken from state.ConfigDefaults().Scope.ManagedRoots.
	StepManagedScope InitStep = "managed_scope"

	// StepReview shows the user a complete summary of what will be written
	// before any file I/O occurs. Confirmation here sets Confirmed = true.
	StepReview InitStep = "review"

	// StepDone is the terminal step displayed after files are successfully
	// written. No further state changes occur after this step.
	StepDone InitStep = "done"
)

// stepOrder is the canonical progression through all steps. NextStep uses this
// to determine the next step after the current one, with optional skipping.
var stepOrder = []InitStep{
	StepWelcome,
	StepGameVersion,
	StepPlatform,
	StepPlatformVersion,
	StepManagedScope,
	StepReview,
	StepDone,
}

// Constants for conflict resolution when pre-existing .lucy/ files are detected.

// ConflictMode determines how init behaves when it detects that one or more
// .lucy/ files already exist.
type ConflictMode string

const (
	// PreserveExisting keeps any file that already exists on disk and only
	// scaffolds the missing ones. This is the default and makes init
	// idempotent: running it twice produces no destructive change.
	PreserveExisting ConflictMode = "preserve"

	// AbortOnConflict refuses to write anything if ANY target file already
	// exists. The user must resolve manually or choose a different mode.
	AbortOnConflict ConflictMode = "abort"

	// OverwriteAll writes all files regardless of what currently exists on disk.
	// Existing content is replaced. The user must explicitly opt into this mode.
	OverwriteAll ConflictMode = "overwrite"
)

// Types for representing the flow state and results.

// InitFlowState holds the mutable state accumulated as the user progresses
// through the init flow. It is passed by pointer through every step so that
// both the interactive TUI and the non-interactive fast path share one model.
type InitFlowState struct {
	// CurrentStep is the step the flow is currently on.
	CurrentStep InitStep

	// GameVersion is the Minecraft game version the user entered (e.g. "1.21.4").
	GameVersion string

	// Platform is the chosen server platform identifier.
	// Valid values: "fabric", "neoforge", "forge", "mcdr", "none"
	Platform string

	// PlatformVersion is the chosen loader/platform version.
	// Empty when Platform == "none" or when the user skips the step.
	PlatformVersion string

	// ManagedRoots is the list of relative directory paths Lucy will manage.
	// Populated from config defaults on construction; the user may edit it in
	// StepManagedScope.
	ManagedRoots []string

	// SourcePriority is the ordered list of package sources.
	// Populated from config defaults on construction; the user may reorder in
	// StepSources.
	SourcePriority []string

	// Confirmed is true only after the user explicitly approves the summary at
	// StepReview. No file I/O must occur before this is true.
	Confirmed bool

	// Aborted is true if the user cancelled the flow before StepReview +
	// Confirmed=true. When true, no files have been written.
	Aborted bool

	// ExistingFiles lists the .lucy/ state files that were already present on
	// disk when NewInitFlowState was called.
	ExistingFiles []string

	// ExistingStateConflicts lists existing state files that could not be safely
	// preserved because they were unreadable or invalid.
	ExistingStateConflicts []string

	// ConflictResolution controls how init handles the ExistingFiles.
	// Default: PreserveExisting.
	ConflictResolution ConflictMode

	// DiscoveredDefaults stores lightweight server-directory guesses that init
	// can use as scaffolding defaults before any file is written.
	DiscoveredDefaults DiscoveredDefaults

	// workDir is the project root checked during construction.
	workDir string
}

// NewInitFlowState constructs an InitFlowState for the given working directory.
// It probes the .lucy/ directory for pre-existing files and populates defaults
// from state.ConfigDefaults().
func NewInitFlowState(workDir string) *InitFlowState {
	defaults := state.ConfigDefaults()
	discovered := DiscoverServerDefaults(workDir)

	s := &InitFlowState{
		CurrentStep:        StepWelcome,
		SourcePriority:     defaults.Sources.Priority,
		ConflictResolution: PreserveExisting,
		DiscoveredDefaults: discovered,
		workDir:            workDir,
	}
	ApplyDiscoveredDefaults(s, discovered)
	if len(s.ManagedRoots) == 0 {
		s.ManagedRoots = append([]string(nil), defaults.Scope.ManagedRoots...)
	}

	// Discover which target state files already exist.
	targets := []string{
		string(state.ConfigFile),
		string(state.ManifestFile),
		string(state.LockFile),
	}
	for _, rel := range targets {
		abs := filepath.Join(workDir, rel)
		if _, err := os.Stat(abs); err == nil {
			s.ExistingFiles = append(s.ExistingFiles, rel)
		}
	}

	if _, exists := containsExistingFile(s.ExistingFiles, string(state.ConfigFile)); exists {
		config, _, err := state.ReadConfig(workDir)
		if err != nil {
			s.ExistingStateConflicts = append(s.ExistingStateConflicts, formatExistingStateConflict(state.ConfigFile, err))
		} else if config != nil {
			if len(s.ManagedRoots) == 0 && len(config.Scope.ManagedRoots) > 0 {
				s.ManagedRoots = append([]string(nil), config.Scope.ManagedRoots...)
			}
			if len(config.Sources.Priority) > 0 {
				s.SourcePriority = append([]string(nil), config.Sources.Priority...)
			}
		}
	}

	if _, exists := containsExistingFile(s.ExistingFiles, string(state.ManifestFile)); exists {
		manifest, _, err := state.ReadManifest(workDir)
		if err != nil {
			s.ExistingStateConflicts = append(s.ExistingStateConflicts, formatExistingStateConflict(state.ManifestFile, err))
		} else if manifest != nil {
			if strings.TrimSpace(manifest.Environment.GameVersion) != "" {
				s.GameVersion = strings.TrimSpace(manifest.Environment.GameVersion)
			}
			if strings.TrimSpace(manifest.Environment.Platform) != "" {
				s.Platform = strings.TrimSpace(manifest.Environment.Platform)
			}
			if strings.TrimSpace(manifest.Environment.PlatformVersion) != "" {
				s.PlatformVersion = strings.TrimSpace(manifest.Environment.PlatformVersion)
			}
			if len(manifest.Policy.ManagedRoots) > 0 {
				s.ManagedRoots = append([]string(nil), manifest.Policy.ManagedRoots...)
			}
		}
	}

	if _, exists := containsExistingFile(s.ExistingFiles, string(state.LockFile)); exists {
		if _, _, err := state.ReadLock(workDir); err != nil {
			s.ExistingStateConflicts = append(s.ExistingStateConflicts, formatExistingStateConflict(state.LockFile, err))
		}
	}

	return s
}

func containsExistingFile(files []string, want string) (int, bool) {
	for i, file := range files {
		if file == want {
			return i, true
		}
	}
	return -1, false
}

func formatExistingStateConflict(file state.StateFile, err error) string {
	return fmt.Sprintf("%s exists but could not be preserved safely: %v", file, err)
}

// Step machine logic

// NextStep returns the step that should follow the current state, applying any
// conditional skips. It does not mutate s; the caller is responsible for
// updating s.CurrentStep.
//
// Rules:
//   - StepPlatformVersion is skipped when Platform == "" or Platform == "none".
//   - StepDone has no successor; returning StepDone from StepDone is a no-op
//     sentinel.
func NextStep(s *InitFlowState) InitStep {
	cur := s.CurrentStep
	for i, step := range stepOrder {
		if step != cur {
			continue
		}
		// Found current step; find the next non-skipped step.
		for _, next := range stepOrder[i+1:] {
			if shouldSkip(s, next) {
				continue
			}
			return next
		}
		// No non-skipped step found after current — stay at StepDone.
		return StepDone
	}
	// currentStep not found in order (shouldn't happen); default to done.
	return StepDone
}

// shouldSkip reports whether step should be skipped given the current flow
// state.
func shouldSkip(s *InitFlowState, step InitStep) bool {
	switch step {
	case StepPlatformVersion:
		// Skip if no platform was selected or platform is vanilla/none.
		return s.Platform == "" || s.Platform == "none"
	}
	return false
}

// CanProceed reports whether enough information has been collected to write
// valid state files. The minimum required fields are GameVersion and a
// decision on ManagedRoots.
//
// CanProceed does NOT check Confirmed; callers must also verify that before
// performing any I/O.
func CanProceed(s *InitFlowState) bool {
	if s.GameVersion == "" {
		return false
	}
	if len(s.ManagedRoots) == 0 {
		return false
	}
	return true
}

// Types for the final result of the flow and error conditions during result construction.

type Manifest = state.Manifest

type Lock = state.Lock

// InitFlowResult is returned by BuildResult once the user has confirmed. It
// describes exactly what will be written and what will be preserved.
type InitFlowResult struct {
	// ConfigToWrite is the Config value that init will marshal to
	// .lucy/config.toml. Nil means the existing file will be preserved
	// (ConflictResolution == PreserveExisting and the file was found).
	ConfigToWrite *state.Config

	// ManifestToWrite is the Manifest that init will marshal to
	// .lucy/manifest.toml. Nil means preserve existing.
	ManifestToWrite *Manifest

	// LockToWrite is the empty Lock skeleton that init scaffolds in
	// .lucy/lock.json. Nil means preserve existing.
	LockToWrite *Lock

	// SkippedFiles lists the state-file paths that were preserved because
	// ConflictResolution == PreserveExisting and they already existed.
	SkippedFiles []string

	// WrittenFiles lists the state-file paths that will be (or were) written.
	WrittenFiles []string
}

// BuildResult constructs an InitFlowResult from the completed flow state.
// It respects ConflictResolution and returns an error if AbortOnConflict
// would be violated or if CanProceed returns false.
//
// BuildResult does NOT perform any file I/O. It only produces a plan.
// The actual writes are performed by the caller.
func BuildResult(s *InitFlowState) (InitFlowResult, error) {
	if !CanProceed(s) {
		return InitFlowResult{}, &ErrFlowIncomplete{State: s}
	}
	if len(s.ExistingStateConflicts) > 0 {
		return InitFlowResult{}, &ErrConflict{
			Mode:          s.ConflictResolution,
			ConflictFiles: append([]string(nil), s.ExistingStateConflicts...),
		}
	}

	existingSet := make(map[string]bool, len(s.ExistingFiles))
	for _, f := range s.ExistingFiles {
		existingSet[f] = true
	}

	// AbortOnConflict: refuse if any target file already exists.
	if s.ConflictResolution == AbortOnConflict && len(s.ExistingFiles) > 0 {
		return InitFlowResult{}, &ErrConflict{
			Mode:          AbortOnConflict,
			ConflictFiles: s.ExistingFiles,
		}
	}

	result := InitFlowResult{}

	// Helper: decide whether to write a given state file.
	willWrite := func(rel string) bool {
		if s.ConflictResolution == OverwriteAll {
			return true
		}
		// PreserveExisting: write only if file was NOT found on disk.
		return !existingSet[rel]
	}

	// config.toml
	cfgPath := string(state.ConfigFile)
	if willWrite(cfgPath) {
		cfg := state.ConfigDefaults()
		cfg.Scope.ManagedRoots = s.ManagedRoots
		cfg.Sources.Priority = s.SourcePriority
		result.ConfigToWrite = &cfg
		result.WrittenFiles = append(result.WrittenFiles, cfgPath)
	} else {
		result.SkippedFiles = append(result.SkippedFiles, cfgPath)
	}

	// manifest.toml
	mfPath := string(state.ManifestFile)
	if willWrite(mfPath) {
		mf := state.ManifestDefaults()
		mf.Environment.GameVersion = s.GameVersion
		mf.Environment.Platform = s.Platform
		mf.Environment.PlatformVersion = s.PlatformVersion
		mf.Policy.ManagedRoots = s.ManagedRoots
		result.ManifestToWrite = &mf
		result.WrittenFiles = append(result.WrittenFiles, mfPath)
	} else {
		result.SkippedFiles = append(result.SkippedFiles, mfPath)
	}

	// lock.json
	lkPath := string(state.LockFile)
	if willWrite(lkPath) {
		lk := state.NewLock()
		if result.ManifestToWrite != nil {
			if data, err := state.SerializeManifest(result.ManifestToWrite); err == nil {
				sum := sha256.Sum256(data)
				lk.ManifestFingerprint = "sha256:" + hex.EncodeToString(sum[:])
			}
			lk.GameVersion = result.ManifestToWrite.Environment.GameVersion
			lk.Platform = result.ManifestToWrite.Environment.Platform
			lk.PlatformVersion = result.ManifestToWrite.Environment.PlatformVersion
		}
		result.LockToWrite = &lk
		result.WrittenFiles = append(result.WrittenFiles, lkPath)
	} else {
		result.SkippedFiles = append(result.SkippedFiles, lkPath)
	}

	return result, nil
}

// Errors for flow validation and conflict detection.

// ErrFlowIncomplete is returned when BuildResult is called on an incomplete
// flow state (CanProceed returns false).
type ErrFlowIncomplete struct {
	State *InitFlowState
}

func (e *ErrFlowIncomplete) Error() string {
	return "init flow is incomplete: game version and managed roots are required"
}

// ErrConflict is returned when ConflictResolution == AbortOnConflict and one
// or more target files already exist.
type ErrConflict struct {
	Mode          ConflictMode
	ConflictFiles []string
}

func (e *ErrConflict) Error() string {
	return "init aborted: one or more .lucy/ files already exist (use --conflict=overwrite to replace or --conflict=preserve to keep them)"
}
