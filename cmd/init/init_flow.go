// Package init is the minimal scaffold backing the `lucy init` command.
//
// The interactive multi-step UX and takeover-class discovery machinery that
// used to live here have been removed ahead of a redesign. What remains is the
// smallest possible state + result model the command still needs to ask a few
// questions, validate the answers, and produce the manifest + lock skeleton
// that ProjectStateService writes to disk.
//
// This file contains no huh/bubbletea TUI code on purpose: the flow logic stays
// pure and testable without a terminal. See tui.go for the interactive driver.
package init

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/mclucy/lucy/state"
	"github.com/mclucy/lucy/workspace"
)

// ConflictMode determines how init behaves when it detects that one or more
// lucy.yaml / lucy-lock.yaml files already exist.
type ConflictMode string

const (
	// PreserveExisting keeps any file that already exists on disk and only
	// scaffolds the missing ones. This is the default and makes init
	// idempotent: running it twice produces no destructive change.
	PreserveExisting ConflictMode = "preserve"

	// AbortOnConflict refuses to write anything if ANY target file already
	// exists. The user must resolve manually or choose a different mode.
	AbortOnConflict ConflictMode = "abort"

	// OverwriteAll writes all files regardless of what currently exists on
	// disk. Existing content is replaced. The user must explicitly opt into
	// this mode.
	OverwriteAll ConflictMode = "overwrite"
)

// InitFlowState holds the mutable state accumulated as the user progresses
// through the init flow. It is passed by pointer through every step so that
// both the interactive TUI and the non-interactive fast path share one model.
type InitFlowState struct {
	// GameVersion is the Minecraft game version the user entered (e.g. "1.21.4").
	GameVersion string

	// Platform is the chosen server platform identifier.
	// Valid values: "fabric", "neoforge", "forge", "mcdr", "none".
	Platform string

	// PlatformVersion is the chosen loader/platform version. Empty when
	// Platform == "none" or when the user leaves it to "latest".
	PlatformVersion string

	// CompatiblePlatforms are extra compatible ecosystems/controller layers
	// that can coexist with the primary runtime (e.g. neoforge + sinytra).
	CompatiblePlatforms []string

	// ConflictResolution controls how init handles ExistingFiles. Default:
	// PreserveExisting.
	ConflictResolution ConflictMode

	// Confirmed is true only after the user explicitly approves the summary at
	// the review step. No file I/O must occur before this is true.
	Confirmed bool

	// Aborted is true if the user cancelled the flow before Confirmed=true.
	// When true, no files have been written.
	Aborted bool

	// ExistingFiles lists the lucy.yaml / lucy-lock.yaml files that were
	// already present on disk when NewInitFlowState was called.
	ExistingFiles []string

	// workDir is the project root checked during construction.
	workDir string
}

// NewInitFlowState constructs an InitFlowState for the given working directory.
// It records which target state files already exist so BuildResult can respect
// ConflictResolution. Discovery is intentionally out of scope for this scaffold
// and will be reintroduced by the upcoming UX redesign.
func NewInitFlowState(workDir string) *InitFlowState {
	s := &InitFlowState{
		ConflictResolution: PreserveExisting,
		workDir:            workDir,
	}

	for _, rel := range []string{
		string(state.ManifestFile),
		string(state.LockFile),
	} {
		if _, err := os.Stat(filepath.Join(workDir, rel)); err == nil {
			s.ExistingFiles = append(s.ExistingFiles, rel)
		}
	}

	return s
}

// CanProceed reports whether enough information has been collected to write
// valid state files. The minimum required field is GameVersion; the platform
// selection (primary + compatible) must also pass state-level validation.
//
// CanProceed does NOT check Confirmed; callers must verify that separately
// before performing any I/O.
func CanProceed(s *InitFlowState) bool {
	if s.GameVersion == "" {
		return false
	}
	return ValidatePlatformSelection(s.Platform, s.CompatiblePlatforms) == nil
}

// ValidatePlatformSelection delegates to state-level validation so init stays
// consistent with the manifest rules for primary + compatible platforms.
func ValidatePlatformSelection(primary string, compatible []string) error {
	return state.ValidateManifestEnvironment(
		state.ManifestEnvironment{
			ModdingPlatform:     primary,
			CompatiblePlatforms: compatible,
		},
	)
}

// RefreshObservedStateAfterInitWrites refreshes probe state for the initialized
// directory so subsequent takeover/status reads see post-init filesystem reality
// rather than stale memoized observations.
func RefreshObservedStateAfterInitWrites(workDir string) {
	workspace.RefreshServerInfo(workDir)
}

// Type aliases kept so cmd/cmd_init.go and any future consumers don't have to
// import state directly through init.
type (
	Manifest = state.Manifest
	Lock     = state.Lock
)

// InitFlowResult is returned by BuildResult once the user has confirmed. It
// describes exactly what will be written and what will be preserved.
type InitFlowResult struct {
	// ManifestToWrite is the Manifest that init will marshal to lucy.yaml.
	// Nil means preserve existing.
	ManifestToWrite *Manifest

	// LockToWrite is the empty Lock skeleton that init scaffolds in
	// lucy-lock.yaml. Nil means preserve existing.
	LockToWrite *Lock

	// SkippedFiles lists state-file paths preserved because
	// ConflictResolution == PreserveExisting and they already existed.
	SkippedFiles []string

	// WrittenFiles lists state-file paths that will be (or were) written.
	WrittenFiles []string
}

// BuildResult constructs an InitFlowResult from the completed flow state. It
// respects ConflictResolution and returns an error if the flow is incomplete
// or AbortOnConflict would be violated.
//
// BuildResult does NOT perform any file I/O. It only produces a plan; the
// caller is responsible for writing via ProjectStateService.
func BuildResult(s *InitFlowState) (InitFlowResult, error) {
	if !CanProceed(s) {
		return InitFlowResult{}, ErrFlowIncomplete
	}

	if s.ConflictResolution == AbortOnConflict && len(s.ExistingFiles) > 0 {
		return InitFlowResult{}, &ErrConflict{
			Mode:          AbortOnConflict,
			ConflictFiles: append([]string(nil), s.ExistingFiles...),
		}
	}

	existing := make(map[string]bool, len(s.ExistingFiles))
	for _, f := range s.ExistingFiles {
		existing[f] = true
	}
	// willWrite mirrors the earlier PreserveExisting vs OverwriteAll semantics:
	// overwrite wins regardless of what's on disk, preserve only fills gaps.
	willWrite := func(rel string) bool {
		if s.ConflictResolution == OverwriteAll {
			return true
		}
		return !existing[rel]
	}

	var result InitFlowResult

	if mfPath := string(state.ManifestFile); willWrite(mfPath) {
		mf := state.ManifestDefaults()
		cfg := state.ConfigDefaults()
		mf.Config = &cfg
		mf.Environment.GameVersion = s.GameVersion
		mf.Environment.ModdingPlatform = s.Platform
		mf.Environment.ModdingPlatformVersion = s.PlatformVersion
		mf.Environment.CompatiblePlatforms = append(
			[]string(nil),
			s.CompatiblePlatforms...,
		)
		result.ManifestToWrite = &mf
		result.WrittenFiles = append(result.WrittenFiles, mfPath)
	} else {
		result.SkippedFiles = append(result.SkippedFiles, mfPath)
	}

	if lkPath := string(state.LockFile); willWrite(lkPath) {
		lk := state.NewLock()
		lk.GameVersion = s.GameVersion
		lk.Platform = s.Platform
		lk.PlatformVersion = s.PlatformVersion
		result.LockToWrite = &lk
		result.WrittenFiles = append(result.WrittenFiles, lkPath)
	} else {
		result.SkippedFiles = append(result.SkippedFiles, lkPath)
	}

	return result, nil
}

// ErrFlowIncomplete is returned when BuildResult is called on a state where
// CanProceed is false.
var ErrFlowIncomplete = errors.New(
	"init flow is incomplete: game version is required",
)

// ErrConflict is returned when ConflictResolution == AbortOnConflict and one or
// more target files already exist.
type ErrConflict struct {
	Mode          ConflictMode
	ConflictFiles []string
}

func (e *ErrConflict) Error() string {
	return "init aborted: one or more lucy.yaml / lucy-lock.yaml files already exist (use --conflict=overwrite to replace or --conflict=preserve to keep them)"
}
