package cli

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/filetrack"
	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/internal/cli/help"
	"github.com/mclucy/lucy/internal/fsutil"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/operation"
	"github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/upstream/routing"
	"github.com/spf13/cobra"
)

// bisectJSON is the shared machine-readable result of every bisect leaf: they
// all report progress through bisectOutput.
var bisectJSON JSONResult[bisectOutput]

func NewBisectCommand(
	contracts *JSONContracts,
	inv *Invocation,
	deps *Dependencies,
) *cobra.Command {
	bisectStartCmd := &cobra.Command{
		Use:   "start",
		Short: help.BisectStartShort,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return actionBisectStart(cmd, inv, deps, args)
		},
	}
	bisectGoodCmd := &cobra.Command{
		Use:   "good",
		Short: help.BisectGoodShort,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return actionBisectGood(cmd, inv, deps, args)
		},
	}
	bisectBadCmd := &cobra.Command{
		Use:   "bad",
		Short: help.BisectBadShort,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return actionBisectBad(cmd, inv, deps, args)
		},
	}
	bisectStatusCmd := &cobra.Command{
		Use:   "status",
		Short: help.BisectStatusShort,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return actionBisectStatus(cmd, inv, deps, args)
		},
	}
	bisectResetCmd := &cobra.Command{
		Use:   "reset",
		Short: help.BisectResetShort,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return actionBisectReset(cmd, inv, deps, args)
		},
	}
	bisectCmd := &cobra.Command{
		Use:   "bisect",
		Short: help.BisectShort,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	MarkHelpOnly(bisectCmd)
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

const (
	bisectFilename     = "bisect.json"
	bisectLockFilename = "bisect.lock"
)

// bisectPhase is the durable phase of a session. A session starts active and
// ends in exactly one terminal phase; only reset returns the workspace to
// having no session.
type bisectPhase string

const (
	bisectPhaseActive    bisectPhase = "active"
	bisectPhaseFound     bisectPhase = "found"
	bisectPhaseExhausted bisectPhase = "exhausted"
)

// bisectStep names the leaf that owns a pending transition. A retry must run
// the same leaf, so the step is recorded instead of inferred from the range.
type bisectStep string

const (
	bisectStepStart bisectStep = "start"
	bisectStepGood  bisectStep = "good"
	bisectStepBad   bisectStep = "bad"
	bisectStepReset bisectStep = "reset"
)

// bisectPending is the durable intent of a transition whose manifest write
// and installation sync have not both completed. The target phase and range
// live on bisectState; this record names the command to rerun and the manifest
// digest the transition intended to publish. The midpoint it marked follows
// from the target range: good advanced Left past it, bad moved Right onto it.
type bisectPending struct {
	Step       bisectStep `json:"step"`
	Digest     string     `json:"digest,omitempty"`
	Resolution string     `json:"resolution,omitempty"`
}

// bisectState is the persisted session. Original is the manifest as it stood
// before the first toggle and is retained until reset has restored and synced
// it, so an interrupted session never loses the manifest it must return to.
type bisectState struct {
	Phase    bisectPhase        `json:"phase"`
	Mods     []bisectMod        `json:"mods"`
	Original *document.Manifest `json:"original"`
	Left     int                `json:"left"`
	Right    int                `json:"right"`

	// Applied is the digest of the manifest the last successful transition
	// left in the workspace. Verdicts and reset refuse to overwrite a manifest
	// that no longer matches it, the pending target, or the original.
	Applied string         `json:"applied,omitempty"`
	Pending *bisectPending `json:"pending,omitempty"`
}

type bisectOutput struct {
	Message  string      `json:"message"`
	Phase    bisectPhase `json:"phase,omitempty"`
	Pending  bisectStep  `json:"pending,omitempty"`
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
	return filepath.Join(workDir, bisectFilename)
}

func bisectLockPath(workDir string) string {
	return filepath.Join(workDir, ".lucy", bisectLockFilename)
}

// bisectStatePresent checks for a session without creating workspace state.
func bisectStatePresent(workDir string) (bool, error) {
	_, err := os.Stat(bisectFilePath(workDir))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, fmt.Errorf("inspect bisect state: %w", err)
}

// requireBisectSession rejects a command that cannot have a session before the
// lock creates .lucy in a directory that is not a bisect workspace.
func requireBisectSession(workDir string) error {
	present, err := bisectStatePresent(workDir)
	if err != nil {
		return err
	}
	if !present {
		return fmt.Errorf("no bisect session found, run `lucy bisect start` first")
	}
	return nil
}

// requireBisectStartWorkspace rejects start before locking when there is
// neither a session to resume nor a manifest to capture. A lock-only
// workspace cannot start a bisect session because Original would be missing.
func requireBisectStartWorkspace(workDir string) error {
	present, err := bisectStatePresent(workDir)
	if err != nil {
		return err
	}
	if present {
		return nil
	}
	manifest := document.ManifestFile(workDir).Path()
	if _, err := os.Stat(manifest); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("read manifest: %w", err)
		}
		return fmt.Errorf("inspect manifest: %w", err)
	}
	return nil
}

// lockBisectSession serializes every session transition across processes. It
// waits instead of failing so a concurrent start, verdict, status, or reset
// observes the previous command's committed state, and it stops waiting when
// the command context ends so a canceled command cannot later mutate the
// session. require runs before .lucy is created, so a command that cannot have
// a session or manifest fails without leaving a workspace marker. It is
// separate from the workspace writer lock because installation sync takes that
// lock itself.
func lockBisectSession(
	ctx context.Context,
	workDir string,
	require func(string) error,
) (*fsutil.FileLock, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := require(workDir); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	relative := filepath.Join(".lucy", bisectLockFilename)
	if err := workspacewrite.SafePath(workDir, relative); err != nil {
		return nil, err
	}
	if err := fsutil.MkdirAllPrivate(filepath.Dir(bisectLockPath(workDir))); err != nil {
		return nil, fmt.Errorf("create workspace state directory: %w", err)
	}
	if err := workspacewrite.SafePath(workDir, relative); err != nil {
		return nil, err
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		lock, err := fsutil.TryLockFile(bisectLockPath(workDir))
		if err == nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, errors.Join(ctxErr, lock.Close())
			}
			return lock, nil
		}
		if !errors.Is(err, fsutil.ErrLocked) {
			return nil, fmt.Errorf("lock bisect session: %w", err)
		}
		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, fmt.Errorf("wait for bisect session lock: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

// readBisectState returns the session, or nil when this workspace has none. A
// corrupt state is an error rather than an empty session: replacing it would
// lose the manifest that reset must restore.
func readBisectState(workDir string) (*bisectState, error) {
	data, err := os.ReadFile(bisectFilePath(workDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read bisect state: %w", err)
	}
	var session bisectState
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf(
			"failed to parse bisect state: %w; the captured original, if present, remains in %s; extract its original value to restore lucy.yaml manually, then delete %s",
			err,
			bisectFilename,
			bisectFilename,
		)
	}
	if err := validateBisectState(&session); err != nil {
		return nil, fmt.Errorf(
			"%w; the captured original, if present, remains in %s; extract its original value to restore lucy.yaml manually, then delete %s",
			err,
			bisectFilename,
			bisectFilename,
		)
	}
	return &session, nil
}

// loadBisectSession reads the session and reports an actionable error when the
// workspace has none.
func loadBisectSession(workDir string) (*bisectState, error) {
	session, err := readBisectState(workDir)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, fmt.Errorf("no bisect session found, run `lucy bisect start` first")
	}
	return session, nil
}

func bisectStateBytes(session *bisectState) ([]byte, error) {
	data, err := json.Marshal(session, jsontext.Multiline(true), jsontext.WithIndent("  "))
	if err != nil {
		return nil, fmt.Errorf("failed to serialize bisect state: %w", err)
	}
	return data, nil
}

func writeBisectState(workDir string, session *bisectState) error {
	if err := validateBisectState(session); err != nil {
		return err
	}
	data, err := bisectStateBytes(session)
	if err != nil {
		return err
	}
	if err := fsutil.AtomicWrite(bisectFilePath(workDir), data, 0o600); err != nil {
		return fmt.Errorf("failed to write bisect state: %w", err)
	}
	return nil
}

// createBisectState publishes the first state of a session without replacing
// one that another process may have created since the caller checked.
func createBisectState(workDir string, session *bisectState) (resultErr error) {
	if err := validateBisectState(session); err != nil {
		return err
	}
	data, err := bisectStateBytes(session)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(workDir)
	if err != nil {
		return fmt.Errorf("open workspace: %w", err)
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
	if err := fsutil.WriteRoot(
		root,
		bisectFilename,
		bytes.NewReader(data),
		fsutil.WriteOptions{Mode: 0o600},
	); err != nil {
		return fmt.Errorf("failed to create bisect state: %w", err)
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
	if session.Pending == nil ||
		(session.Pending.Step != bisectStepStart &&
			session.Pending.Step != bisectStepReset) {
		if session.Applied == "" {
			return fmt.Errorf("invalid bisect state: no committed manifest digest")
		}
	} else if session.Pending.Step == bisectStepStart && session.Applied != "" {
		return fmt.Errorf("invalid bisect state: interrupted start already has a committed manifest digest")
	}
	switch session.Phase {
	case bisectPhaseActive:
		if session.Left < 0 || session.Right >= len(session.Mods) || session.Left > session.Right {
			return fmt.Errorf(
				"invalid bisect state: active range [%d, %d] outside %d packages",
				session.Left,
				session.Right,
				len(session.Mods),
			)
		}
	case bisectPhaseFound:
		if session.Left != session.Right ||
			session.Left < 0 ||
			session.Left >= len(session.Mods) {
			return fmt.Errorf(
				"invalid bisect state: found range [%d, %d] is not one package",
				session.Left,
				session.Right,
			)
		}
	case bisectPhaseExhausted:
		if session.Right < 0 ||
			session.Right >= len(session.Mods) ||
			session.Left != session.Right+1 {
			return fmt.Errorf(
				"invalid bisect state: exhausted range [%d, %d] is inconsistent",
				session.Left,
				session.Right,
			)
		}
	default:
		return fmt.Errorf("invalid bisect state: unknown phase %q", session.Phase)
	}
	return validateBisectPending(session)
}

func validateBisectPending(session *bisectState) error {
	pending := session.Pending
	if pending == nil {
		return nil
	}
	if pending.Digest == "" || pending.Resolution == "" {
		return fmt.Errorf("invalid bisect state: pending transition records no manifest digest and resolution fingerprint")
	}
	switch pending.Step {
	case bisectStepStart:
		if session.Phase != bisectPhaseActive ||
			session.Left != 0 ||
			session.Right != len(session.Mods)-1 {
			return fmt.Errorf("invalid bisect state: interrupted start does not own the full range")
		}
	case bisectStepGood:
		if session.Left <= 0 ||
			(session.Phase != bisectPhaseActive && session.Phase != bisectPhaseExhausted) {
			return fmt.Errorf("invalid bisect state: interrupted good does not match its range or phase")
		}
	case bisectStepBad:
		if session.Right < 0 ||
			(session.Phase != bisectPhaseActive && session.Phase != bisectPhaseFound) {
			return fmt.Errorf("invalid bisect state: interrupted bad does not match its range or phase")
		}
	case bisectStepReset:
		// Reset can follow any phase; the pending digest names the manifest
		// the failed reset left in place.
	default:
		return fmt.Errorf("invalid bisect state: unknown pending step %q", pending.Step)
	}
	return nil
}

// errBisectManifestChanged refuses to proceed when the manifest no longer
// matches any state the session itself published. Restoring the original would
// otherwise discard an unrelated edit without leaving a trace.
var errBisectManifestChanged = errors.New(
	"lucy.yaml changed outside the bisect session; refusing to overwrite it. " +
		"Undo that change and retry, or recover the captured original from " +
		"bisect.json (mode 0600) and delete bisect.json to abandon the session",
)

var errBisectManifestChangedDuringSync = errors.New(
	"lucy.yaml changed while the installation synced; the bisect transition " +
		"remains pending. Restore lucy.yaml and rerun the command, or recover " +
		"the captured original from bisect.json (mode 0600)",
)

// bisectManifestResolutionFingerprint is the fingerprint installation uses to
// decide whether a lock still answers the manifest.
func bisectManifestResolutionFingerprint(doc *document.Manifest) (string, error) {
	fingerprint, err := doc.Fingerprint()
	if err != nil {
		return "", fmt.Errorf("fingerprint manifest: %w", err)
	}
	return fingerprint, nil
}

// bisectManifestDigest covers every persisted manifest field, including name
// metadata that resolution intentionally ignores. reset and verdicts use it to
// distinguish a manifest the session published from one edited outside it.
func bisectManifestDigest(doc *document.Manifest) (string, error) {
	if err := doc.Normalize(); err != nil {
		return "", fmt.Errorf("normalize manifest: %w", err)
	}
	data, err := json.Marshal(doc, json.Deterministic(true))
	if err != nil {
		return "", fmt.Errorf("digest manifest: %w", err)
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// withBisectWorkspace replays interrupted workspace journals under the shared
// writer lock and runs action while that lock is held. The caller must hold
// the bisect session lock; action must not call installation sync, which takes
// the same writer lock itself.
func withBisectWorkspace(
	ctx context.Context,
	workDir string,
	logger *log.Logger,
	action func() error,
) (resultErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	writer, err := workspacewrite.Lock(workDir, logger)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, writer.Close()) }()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := filetrack.RecoverLocked(workDir, logger); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return action()
}

type bisectManifestRelation int

const (
	bisectManifestForeign bisectManifestRelation = iota
	bisectManifestOwned
	// bisectManifestRenamed means the manifest matches the pending target in
	// every field the resolution fingerprint covers, but differs in a field it
	// intentionally ignores, currently the server name. The caller adopts that
	// name instead of rejecting a rename that cannot affect installation.
	bisectManifestRenamed
)

// classifyBisectManifest reports how doc relates to the states this session
// published. Without a pending transition, a changed manifest is foreign even
// when only its name differs; that edit happened after a committed step and
// reset must not discard it silently.
func classifyBisectManifest(
	session *bisectState,
	doc *document.Manifest,
) (bisectManifestRelation, error) {
	digest, err := bisectManifestDigest(doc)
	if err != nil {
		return bisectManifestForeign, err
	}
	if digest == session.Applied {
		return bisectManifestOwned, nil
	}
	if session.Pending != nil && digest == session.Pending.Digest {
		return bisectManifestOwned, nil
	}
	original, err := bisectManifestDigest(session.Original)
	if err != nil {
		return bisectManifestForeign, err
	}
	if digest == original {
		return bisectManifestOwned, nil
	}
	if session.Pending == nil || session.Pending.Resolution == "" {
		return bisectManifestForeign, nil
	}
	resolution, err := bisectManifestResolutionFingerprint(doc)
	if err != nil {
		return bisectManifestForeign, err
	}
	if resolution == session.Pending.Resolution {
		return bisectManifestRenamed, nil
	}
	return bisectManifestForeign, nil
}

func adoptBisectRename(session *bisectState, doc *document.Manifest) {
	session.Original.Name = doc.Name
}

// toggleBisectRange enables requirements through mid and disables the rest
// after them without writing anything. It returns both the ownership digest
// and the resolution fingerprint machinery pins during sync.
func toggleBisectRange(
	doc *document.Manifest,
	mods []bisectMod,
	mid int,
) (
	enabled, disabled, matched int,
	digest, resolution string,
	err error,
) {
	for i, mod := range mods {
		if setRequirementEnabled(doc, mod, i <= mid) {
			matched++
			if i <= mid {
				enabled++
			} else {
				disabled++
			}
		}
	}
	digest, err = bisectManifestDigest(doc)
	if err != nil {
		return 0, 0, 0, "", "", err
	}
	resolution, err = bisectManifestResolutionFingerprint(doc)
	if err != nil {
		return 0, 0, 0, "", "", err
	}
	return enabled, disabled, matched, digest, resolution, nil
}

// prepareBisectRangeLocked writes the target digest to the pending transition
// before changing the manifest, then writes the range. The caller holds the
// workspace writer lock after journal recovery.
func prepareBisectRangeLocked(
	ctx context.Context,
	workDir string,
	session *bisectState,
) (enabled, disabled int, digest, resolution string, err error) {
	if err := ctx.Err(); err != nil {
		return 0, 0, "", "", err
	}
	if session.Pending == nil {
		return 0, 0, "", "", fmt.Errorf("invalid bisect state: no pending transition")
	}
	doc, err := document.ManifestFile(workDir).Read()
	if err != nil {
		return 0, 0, "", "", fmt.Errorf("read manifest: %w", err)
	}
	relation, err := classifyBisectManifest(session, doc)
	if err != nil {
		return 0, 0, "", "", err
	}
	switch relation {
	case bisectManifestForeign:
		return 0, 0, "", "", errBisectManifestChanged
	case bisectManifestRenamed:
		adoptBisectRename(session, doc)
	}
	mid := (session.Left + session.Right) / 2
	var matched int
	enabled, disabled, matched, digest, resolution, err = toggleBisectRange(doc, session.Mods, mid)
	if err != nil {
		return 0, 0, "", "", err
	}
	session.Pending.Digest = digest
	session.Pending.Resolution = resolution
	if err := ctx.Err(); err != nil {
		return 0, 0, "", "", err
	}
	if err := writeBisectState(workDir, session); err != nil {
		return 0, 0, "", "", err
	}
	if matched > 0 {
		if err := ctx.Err(); err != nil {
			return 0, 0, "", "", err
		}
		if err := document.ManifestFile(workDir).Write(doc); err != nil {
			return 0, 0, "", "", fmt.Errorf("write manifest: %w", err)
		}
	}
	return enabled, disabled, digest, resolution, nil
}

// prepareBisectRange stages the pending range. The caller syncs afterwards,
// once the workspace writer lock is released, because installation takes that
// lock itself.
func prepareBisectRange(
	ctx context.Context,
	workDir string,
	logger *log.Logger,
	session *bisectState,
) (enabled, disabled int, digest, resolution string, err error) {
	err = withBisectWorkspace(ctx, workDir, logger, func() error {
		var actionErr error
		enabled, disabled, digest, resolution, actionErr = prepareBisectRangeLocked(ctx, workDir, session)
		return actionErr
	})
	return enabled, disabled, digest, resolution, err
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

// writeBisectOriginal restores the manifest captured before the first toggle.
// It records step and the target digest on the pending transition before
// writing, then writes the original. The caller syncs afterwards; a retry
// after a failed sync must run that sync even when the manifest already
// matches.
func writeBisectOriginal(
	ctx context.Context,
	workDir string,
	logger *log.Logger,
	session *bisectState,
	step bisectStep,
) (digest, resolution string, err error) {
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	err = withBisectWorkspace(ctx, workDir, logger, func() error {
		current, err := document.ManifestFile(workDir).Read()
		if err != nil {
			return fmt.Errorf("read manifest: %w", err)
		}
		relation, err := classifyBisectManifest(session, current)
		if err != nil {
			return err
		}
		switch relation {
		case bisectManifestForeign:
			return errBisectManifestChanged
		case bisectManifestRenamed:
			adoptBisectRename(session, current)
		}
		originalDigest, err := bisectManifestDigest(session.Original)
		if err != nil {
			return err
		}
		originalResolution, err := bisectManifestResolutionFingerprint(session.Original)
		if err != nil {
			return err
		}
		digest = originalDigest
		resolution = originalResolution
		switch {
		case session.Pending == nil:
			session.Pending = &bisectPending{
				Step:       step,
				Digest:     originalDigest,
				Resolution: originalResolution,
			}
		case step == bisectStepReset:
			// Reset supersedes an interrupted leaf. Keep that leaf's target
			// digest until the original is written, so a retry can still
			// recognize the manifest the failed reset left in place.
			session.Pending.Step = bisectStepReset
			if session.Pending.Digest == "" {
				session.Pending.Digest = originalDigest
			}
			if session.Pending.Resolution == "" {
				session.Pending.Resolution = originalResolution
			}
		default:
			session.Pending.Step = step
			session.Pending.Digest = originalDigest
			session.Pending.Resolution = originalResolution
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := writeBisectState(workDir, session); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := document.ManifestFile(workDir).Write(session.Original); err != nil {
			return fmt.Errorf("restore manifest: %w", err)
		}
		return nil
	})
	return digest, resolution, err
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
		Total: len(session.Mods),
		Left:  session.Left,
		Right: session.Right,
	}
	if session.Phase != bisectPhaseActive {
		return view
	}
	view.Remaining = session.Right - session.Left + 1
	mid := (session.Left + session.Right) / 2
	view.Midpoint = mid
	view.Candidate = &session.Mods[mid]
	return view
}

func outputBisect(
	cmd *cobra.Command,
	inv *Invocation,
	view bisectOutput,
) error {
	output := NewOperationOutput(cmd, inv)
	if cmd.Name() == "status" && cmd.Parent() != nil && cmd.Parent().Name() == "bisect" {
		output = NewQueryOutput(cmd, inv)
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
	if output.Pending == bisectStepReset {
		builder.WriteString("Pending: rerun `lucy bisect reset` to complete the interrupted reset.\n")
	} else if output.Pending != "" {
		builder.WriteString(
			fmt.Sprintf(
				"Pending: rerun `lucy bisect %s` to complete the interrupted transition or `lucy bisect reset` to abandon the session.\n",
				output.Pending,
			),
		)
	}
	if !output.Complete && output.Pending == "" && output.State != nil && output.State.Candidate != nil {
		builder.WriteString("Start the server, test it, then run `lucy bisect good` or `lucy bisect bad`.")
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
// ordered so a package always follows the packages it depends on. doc is the
// recovered snapshot the session records as its original; dependency loading
// may take long enough that start must revalidate the file before publishing.
func bisectCandidates(
	ctx context.Context,
	lookup func(context.Context, upstream.ArtifactFingerprint) routing.ArtifactLookup,
	workDir string,
	doc *document.Manifest,
	output *Output,
) ([]bisectMod, []string, error) {
	graph, _, _, err := LoadDependencyData(ctx, workDir, false, lookup, output)
	if err != nil {
		return nil, nil, err
	}

	var mods []bisectMod
	seen := make(map[string]bool)
	for _, root := range bisectOrderedArtifacts(graph) {
		for _, reference := range bisectNodeReferences(doc, root) {
			if !requirementEnabled(
				doc,
				root.Environment,
				root.Ecosystem,
				reference,
			) {
				continue
			}
			mod := bisectMod{
				Environment: root.Environment,
				Ecosystem:   root.Ecosystem,
				Reference:   reference,
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
	}
	return mods, output.Warnings(), nil
}

// bisectOrderedArtifacts returns every artifact node in dependency-first
// order. Root selection is not enough: in a live observation, a directly
// declared requirement can also be another package's dependency.
//
// The order is computed on the artifact graph. Sorting the mixed graph and
// dropping modules afterwards is not enough: an artifact only becomes ready
// after every module it ships, while a dependent only waits for the module it
// needs, so a package shipping several modules can follow its own dependent.
// Collapsing each module onto its artifact first keeps the edge from that
// dependent to the package that provides the required module.
func bisectOrderedArtifacts(graph *DependencyGraph) []*GraphNode {
	artifacts := make([]*GraphNode, 0, len(graph.Nodes))
	for _, node := range graph.Nodes {
		if !node.Module {
			artifacts = append(artifacts, node)
		}
	}
	compare := func(a, b *GraphNode) int {
		if c := cmp.Compare(a.ID, b.ID); c != 0 {
			return c
		}
		return cmp.Compare(a.key(), b.key())
	}
	byID := func() []*GraphNode {
		slices.SortFunc(artifacts, compare)
		return artifacts
	}

	// A module is represented by the artifact that ships it.
	owner := make(map[string]*GraphNode, len(graph.Nodes))
	for _, node := range graph.Nodes {
		if !node.Module {
			owner[node.key()] = node
			continue
		}
		artifact, ok := graph.Nodes[nodeKey(
			node.Environment,
			node.Ecosystem,
			node.Artifact,
		)]
		if ok {
			owner[node.key()] = artifact
		}
	}

	// Lift edges onto owning artifacts, dropping the tree-only
	// artifact-to-own-module self edges.
	dependencies := make(map[string]map[string]bool, len(artifacts))
	dependents := make(map[string][]*GraphNode, len(artifacts))
	for _, node := range graph.Nodes {
		dependent := owner[node.key()]
		if dependent == nil {
			continue
		}
		dependentKey := dependent.key()
		for _, child := range node.Children {
			dependency := owner[child.key()]
			if dependency == nil || dependency == dependent {
				continue
			}
			dependencyKey := dependency.key()
			if dependencies[dependentKey] == nil {
				dependencies[dependentKey] = make(map[string]bool)
			}
			if dependencies[dependentKey][dependencyKey] {
				continue
			}
			dependencies[dependentKey][dependencyKey] = true
			dependents[dependencyKey] = append(dependents[dependencyKey], dependent)
		}
	}

	remaining := make(map[string]int, len(artifacts))
	queue := make([]*GraphNode, 0, len(artifacts))
	for _, artifact := range artifacts {
		key := artifact.key()
		remaining[key] = len(dependencies[key])
		if remaining[key] == 0 {
			queue = append(queue, artifact)
		}
	}
	slices.SortFunc(queue, compare)

	// Each artifact enters the queue at most once, so insertion reuses the
	// preallocated capacity.
	ordered := make([]*GraphNode, 0, len(artifacts))
	for head := 0; head < len(queue); head++ {
		artifact := queue[head]
		ordered = append(ordered, artifact)
		for _, dependent := range dependents[artifact.key()] {
			dependentKey := dependent.key()
			remaining[dependentKey]--
			if remaining[dependentKey] != 0 {
				continue
			}
			index, _ := slices.BinarySearchFunc(
				queue[head+1:],
				dependent,
				compare,
			)
			queue = slices.Insert(queue, head+1+index, dependent)
		}
	}
	if len(ordered) != len(artifacts) {
		return byID()
	}
	return ordered
}

// bisectNodeReferences returns the manifest requirement references that select
// node. A lock graph carries the exact bindings on the node, including several
// bindings for one artifact. An observation graph has no lock bindings, so the
// manifest references are matched by provider and project identity.
func bisectNodeReferences(doc *document.Manifest, node *GraphNode) []string {
	if len(node.Requirements) > 0 {
		references := slices.Clone(node.Requirements)
		slices.Sort(references)
		return slices.Compact(references)
	}
	var packages map[string]document.Requirement
	switch node.Environment {
	case server.EnvironmentServer:
		packages = doc.Server.Packages[node.Ecosystem]
	case server.EnvironmentMCDR:
		if doc.MCDR != nil {
			packages = doc.MCDR.Packages
		}
	}
	references := make([]string, 0, len(packages))
	for reference := range packages {
		if bisectReferenceMatchesNode(reference, node) {
			references = append(references, reference)
		}
	}
	slices.Sort(references)
	return references
}

func bisectReferenceMatchesNode(reference string, node *GraphNode) bool {
	provider, project, err := document.ParseReference(reference)
	if err != nil {
		return false
	}
	if node.Provider != catalog.ProviderUnspecified && provider != node.Provider {
		return false
	}
	if node.ProjectID != "" {
		return project == node.ProjectID
	}
	if node.Provider != catalog.ProviderUnspecified {
		return strings.TrimPrefix(node.ID, node.Provider.String()+":") == project
	}
	return node.ID == project
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

// completeBisectTransition writes the manifest target recorded by a pending
// transition, syncs the installation, and only then clears the pending record.
// The record is durable before the first manifest write, so a failure at any
// point leaves a state that the same leaf can resume. It returns the result
// the owning leaf should report.
func completeBisectTransition(
	ctx context.Context,
	ops *operation.Service,
	workDir string,
	logger *log.Logger,
	session *bisectState,
) (bisectOutput, error) {
	if err := ctx.Err(); err != nil {
		return bisectOutput{}, err
	}
	pending := session.Pending
	if pending == nil {
		return bisectOutput{}, fmt.Errorf("invalid bisect state: no pending transition")
	}
	var (
		targetDigest     string
		targetResolution string
		enabled          int
		disabled         int
		restored         int
		err              error
	)
	switch session.Phase {
	case bisectPhaseActive:
		enabled, disabled, targetDigest, targetResolution, err = prepareBisectRange(ctx, workDir, logger, session)
	case bisectPhaseFound, bisectPhaseExhausted:
		targetDigest, targetResolution, err = writeBisectOriginal(ctx, workDir, logger, session, pending.Step)
		restored = countRequirements(session.Original)
	default:
		return bisectOutput{}, fmt.Errorf("invalid bisect state: unknown phase %q", session.Phase)
	}
	if err != nil {
		return bisectOutput{}, err
	}
	return commitBisectTransition(
		ctx,
		ops,
		workDir,
		logger,
		session,
		*pending,
		targetDigest,
		targetResolution,
		enabled,
		disabled,
		restored,
	)
}

// commitBisectTransition syncs the staged manifest, then records the committed
// digest and clears the pending transition. A failure before the final write
// leaves both the staged manifest and the pending record for a retry.
func commitBisectTransition(
	ctx context.Context,
	ops *operation.Service,
	workDir string,
	logger *log.Logger,
	session *bisectState,
	pending bisectPending,
	targetDigest string,
	targetResolution string,
	enabled, disabled, restored int,
) (bisectOutput, error) {
	if err := ctx.Err(); err != nil {
		return bisectOutput{}, err
	}
	// Sync even when the manifest already shows the target: the manifest
	// states what should be installed, not what is installed. ExpectedManifest
	// also makes a concurrent edit fail instead of being overwritten by
	// whichever document the sync happened to read.
	result, err := ops.Sync(
		ctx,
		workDir,
		install.Options{ExpectedManifest: targetResolution},
	)
	if err != nil {
		return bisectOutput{}, fmt.Errorf("sync installation: %w", err)
	}
	// The resolution fingerprint intentionally ignores name metadata, so sync
	// can succeed after a concurrent rename. Validate the full digest before
	// clearing pending; otherwise the session would record a stale Applied
	// digest and reject every later command.
	applied := targetDigest
	err = withBisectWorkspace(ctx, workDir, logger, func() error {
		current, err := document.ManifestFile(workDir).Read()
		if err != nil {
			return fmt.Errorf("read manifest after sync: %w", err)
		}
		currentDigest, err := bisectManifestDigest(current)
		if err != nil {
			return err
		}
		if currentDigest != targetDigest {
			currentResolution, err := bisectManifestResolutionFingerprint(current)
			if err != nil {
				return err
			}
			if currentResolution != targetResolution {
				return errBisectManifestChangedDuringSync
			}
			// Only fields resolution ignores changed, currently the server
			// name. Adopt the rename into the original so reset preserves it
			// instead of reverting a change the session already observed.
			adoptBisectRename(session, current)
			applied = currentDigest
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		session.Pending = nil
		session.Applied = applied
		return writeBisectState(workDir, session)
	})
	if err != nil {
		return bisectOutput{}, err
	}
	output := bisectOutput{
		Phase:    session.Phase,
		Complete: session.Phase != bisectPhaseActive,
		State:    currentBisectView(session),
		Enabled:  enabled,
		Disabled: disabled,
		Restored: restored,
		Warnings: syncWarnings(result),
	}
	output.Message = bisectTransitionMessage(session, pending)
	if session.Phase == bisectPhaseFound {
		output.Found = &session.Mods[session.Left]
	}
	return output, nil
}

// bisectTransitionMessage names the outcome of the transition a leaf recorded.
func bisectTransitionMessage(session *bisectState, pending bisectPending) string {
	switch pending.Step {
	case bisectStepStart:
		return "session started"
	case bisectStepGood:
		if session.Phase == bisectPhaseExhausted {
			return "all remaining packages are good; no bad package found"
		}
		return fmt.Sprintf(
			"marked %s good",
			bisectModLabel(session.Mods[session.Left-1]),
		)
	case bisectStepBad:
		if session.Phase == bisectPhaseFound {
			return "found bad package"
		}
		return fmt.Sprintf(
			"marked %s bad",
			bisectModLabel(session.Mods[session.Right]),
		)
	case bisectStepReset:
		return "session reset"
	default:
		return "unknown transition"
	}
}

// bisectInterruptedError refuses a different leaf while a transition is
// pending. The target range already contains the recorded verdict, so letting
// another verdict advance it again would corrupt the search.
func bisectInterruptedError(pending *bisectPending) error {
	if pending.Step == bisectStepReset {
		return fmt.Errorf(
			"bisect reset was interrupted; rerun `lucy bisect reset` to complete it",
		)
	}
	return fmt.Errorf(
		"bisect %s was interrupted; rerun `lucy bisect %s` to complete it or `lucy bisect reset` to abandon the session",
		pending.Step,
		pending.Step,
	)
}

// bisectTerminalError refuses a new verdict after a session reached its
// terminal phase. Only reset may end the session, so the recorded verdict
// cannot silently change.
func bisectTerminalError(session *bisectState) error {
	switch session.Phase {
	case bisectPhaseFound:
		return fmt.Errorf(
			"bisect session already found %s; run `lucy bisect reset` before another verdict",
			bisectModLabel(session.Mods[session.Left]),
		)
	case bisectPhaseExhausted:
		return fmt.Errorf("bisect session already finished with no bad package; run `lucy bisect reset` before another verdict")
	default:
		return nil
	}
}

// bisectStartRefusal explains why start cannot replace an existing session.
// The captured manifest lives only in the session file, so replacing that file
// would make the original unrecoverable.
func bisectStartRefusal(session *bisectState) error {
	if session.Pending != nil {
		if session.Pending.Step == bisectStepReset {
			return fmt.Errorf(
				"bisect reset was interrupted; rerun `lucy bisect reset` before starting another session",
			)
		}
		return fmt.Errorf(
			"bisect session is interrupted during %s; run `lucy bisect %s` to complete it or `lucy bisect reset` to abandon the session",
			session.Pending.Step,
			session.Pending.Step,
		)
	}
	switch session.Phase {
	case bisectPhaseFound:
		return fmt.Errorf(
			"bisect session already found %s; run `lucy bisect reset` before starting another session",
			bisectModLabel(session.Mods[session.Left]),
		)
	case bisectPhaseExhausted:
		return fmt.Errorf("bisect session already finished with no bad package; run `lucy bisect reset` before starting another session")
	default:
		return fmt.Errorf("bisect session already active; run `lucy bisect reset` before starting another session")
	}
}

func actionBisectStart(
	cmd *cobra.Command,
	inv *Invocation,
	deps *Dependencies,
	_ []string,
) (resultErr error) {
	ctx, stop := CommandContext(cmd)
	defer stop()
	workDir := inv.Directory

	lock, err := lockBisectSession(ctx, workDir, requireBisectStartWorkspace)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, lock.Close()) }()
	if err := ctx.Err(); err != nil {
		return err
	}

	existing, err := readBisectState(workDir)
	if err != nil {
		return err
	}
	if existing != nil {
		if existing.Pending != nil && existing.Pending.Step == bisectStepStart {
			output, err := completeBisectTransition(
				ctx,
				deps.Operations,
				workDir,
				inv.Logger,
				existing,
			)
			if err != nil {
				return err
			}
			return outputBisect(cmd, inv, output)
		}
		return bisectStartRefusal(existing)
	}

	var (
		original       *document.Manifest
		originalDigest string
	)
	if err := withBisectWorkspace(ctx, workDir, inv.Logger, func() error {
		doc, err := document.ManifestFile(workDir).Read()
		if err != nil {
			return fmt.Errorf("read manifest: %w", err)
		}
		original = doc
		originalDigest, err = bisectManifestDigest(doc)
		return err
	}); err != nil {
		return err
	}

	candidateOutput := NewOperationOutput(cmd, inv)
	mods, candidateWarnings, err := bisectCandidates(
		ctx,
		deps.Router.ResolveArtifactByHash,
		workDir,
		original,
		candidateOutput,
	)
	if err != nil {
		return err
	}
	if len(mods) == 0 {
		return outputBisect(
			cmd,
			inv,
			bisectOutput{
				Message:  "no enabled package requirements found in this manifest",
				Complete: true,
				Warnings: candidateWarnings,
			},
		)
	}

	session := &bisectState{
		Phase:    bisectPhaseActive,
		Mods:     mods,
		Original: original,
		Right:    len(mods) - 1,
		Pending:  &bisectPending{Step: bisectStepStart},
	}
	var (
		enabled          int
		disabled         int
		matched          int
		targetDigest     string
		targetResolution string
	)
	if err := withBisectWorkspace(ctx, workDir, inv.Logger, func() error {
		current, err := document.ManifestFile(workDir).Read()
		if err != nil {
			return fmt.Errorf("read manifest: %w", err)
		}
		currentDigest, err := bisectManifestDigest(current)
		if err != nil {
			return err
		}
		if currentDigest != originalDigest {
			return fmt.Errorf(
				"manifest changed during package selection; retry `lucy bisect start`",
			)
		}
		mid := (session.Left + session.Right) / 2
		enabled, disabled, matched, targetDigest, targetResolution, err = toggleBisectRange(current, session.Mods, mid)
		if err != nil {
			return err
		}
		session.Pending.Digest = targetDigest
		session.Pending.Resolution = targetResolution
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := createBisectState(workDir, session); err != nil {
			return err
		}
		if matched > 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := document.ManifestFile(workDir).Write(current); err != nil {
				return fmt.Errorf("write manifest: %w", err)
			}
		}
		return nil
	}); err != nil {
		return err
	}

	output, err := commitBisectTransition(
		ctx,
		deps.Operations,
		workDir,
		inv.Logger,
		session,
		*session.Pending,
		targetDigest,
		targetResolution,
		enabled,
		disabled,
		0,
	)
	if err != nil {
		return err
	}
	output.Warnings = append(candidateWarnings, output.Warnings...)
	return outputBisect(cmd, inv, output)
}

func actionBisectGood(
	cmd *cobra.Command,
	inv *Invocation,
	deps *Dependencies,
	_ []string,
) (resultErr error) {
	ctx, stop := CommandContext(cmd)
	defer stop()
	workDir := inv.Directory

	lock, err := lockBisectSession(ctx, workDir, requireBisectSession)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, lock.Close()) }()
	if err := ctx.Err(); err != nil {
		return err
	}

	session, err := loadBisectSession(workDir)
	if err != nil {
		return err
	}
	if session.Pending != nil {
		if session.Pending.Step != bisectStepGood {
			return bisectInterruptedError(session.Pending)
		}
		output, err := completeBisectTransition(
			ctx,
			deps.Operations,
			workDir,
			inv.Logger,
			session,
		)
		if err != nil {
			return err
		}
		return outputBisect(cmd, inv, output)
	}
	if err := bisectTerminalError(session); err != nil {
		return err
	}

	mid := (session.Left + session.Right) / 2
	session.Left = mid + 1
	session.Pending = &bisectPending{Step: bisectStepGood}
	if session.Left > session.Right {
		session.Phase = bisectPhaseExhausted
	}
	output, err := completeBisectTransition(
		ctx,
		deps.Operations,
		workDir,
		inv.Logger,
		session,
	)
	if err != nil {
		return err
	}
	return outputBisect(cmd, inv, output)
}

func actionBisectBad(
	cmd *cobra.Command,
	inv *Invocation,
	deps *Dependencies,
	_ []string,
) (resultErr error) {
	ctx, stop := CommandContext(cmd)
	defer stop()
	workDir := inv.Directory

	lock, err := lockBisectSession(ctx, workDir, requireBisectSession)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, lock.Close()) }()
	if err := ctx.Err(); err != nil {
		return err
	}

	session, err := loadBisectSession(workDir)
	if err != nil {
		return err
	}
	if session.Pending != nil {
		if session.Pending.Step != bisectStepBad {
			return bisectInterruptedError(session.Pending)
		}
		output, err := completeBisectTransition(
			ctx,
			deps.Operations,
			workDir,
			inv.Logger,
			session,
		)
		if err != nil {
			return err
		}
		return outputBisect(cmd, inv, output)
	}
	if err := bisectTerminalError(session); err != nil {
		return err
	}

	mid := (session.Left + session.Right) / 2
	session.Right = mid
	session.Pending = &bisectPending{Step: bisectStepBad}
	if session.Left == session.Right {
		session.Phase = bisectPhaseFound
	}
	output, err := completeBisectTransition(
		ctx,
		deps.Operations,
		workDir,
		inv.Logger,
		session,
	)
	if err != nil {
		return err
	}
	return outputBisect(cmd, inv, output)
}

func actionBisectStatus(
	cmd *cobra.Command,
	inv *Invocation,
	_ *Dependencies,
	_ []string,
) (resultErr error) {
	ctx, stop := CommandContext(cmd)
	defer stop()
	workDir := inv.Directory

	lock, err := lockBisectSession(ctx, workDir, requireBisectSession)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, lock.Close()) }()
	if err := ctx.Err(); err != nil {
		return err
	}

	session, err := loadBisectSession(workDir)
	if err != nil {
		return err
	}

	output := bisectOutput{
		Phase:    session.Phase,
		Complete: session.Phase != bisectPhaseActive,
		State:    currentBisectView(session),
	}
	if session.Phase == bisectPhaseFound {
		output.Found = &session.Mods[session.Left]
	}
	if session.Pending != nil {
		output.Pending = session.Pending.Step
		output.Message = fmt.Sprintf(
			"session interrupted during %s",
			session.Pending.Step,
		)
	} else {
		switch session.Phase {
		case bisectPhaseFound:
			output.Message = "session complete: bad package identified"
		case bisectPhaseExhausted:
			output.Message = "session complete: no bad package found"
		default:
			output.Message = "session active"
		}
	}
	return outputBisect(cmd, inv, output)
}

func actionBisectReset(
	cmd *cobra.Command,
	inv *Invocation,
	deps *Dependencies,
	_ []string,
) (resultErr error) {
	ctx, stop := CommandContext(cmd)
	defer stop()
	workDir := inv.Directory

	lock, err := lockBisectSession(ctx, workDir, requireBisectSession)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, lock.Close()) }()
	if err := ctx.Err(); err != nil {
		return err
	}

	session, err := loadBisectSession(workDir)
	if err != nil {
		return err
	}
	// Reset supersedes any pending transition: restoring the captured
	// manifest and syncing it is the final state, whatever the interrupted
	// leaf had intended. The session file is kept until this succeeds, so a
	// failed restore remains retryable.
	_, targetResolution, err := writeBisectOriginal(
		ctx,
		workDir,
		inv.Logger,
		session,
		bisectStepReset,
	)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	result, err := deps.Operations.Sync(
		ctx,
		workDir,
		install.Options{ExpectedManifest: targetResolution},
	)
	if err != nil {
		return fmt.Errorf("sync installation: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := deleteBisectState(workDir); err != nil {
		return err
	}

	return outputBisect(
		cmd,
		inv,
		bisectOutput{
			Message:  "session reset",
			Complete: true,
			Restored: countRequirements(session.Original),
			Warnings: syncWarnings(result),
		},
	)
}
