package bisect

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/filetrack"
	"github.com/mclucy/lucy/internal/fsutil"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/server"
)

const (
	stateFilename = "bisect.json"
	lockFilename  = "bisect.lock"
)

// pendingTransition is the durable intent of a transition whose manifest
// write and installation sync have not both completed. The target phase and
// range live on state; this record names the command to rerun and the manifest
// digest the transition intended to publish. The midpoint it marked follows
// from the target range: good advanced Left past it, bad moved Right onto it.
type pendingTransition struct {
	Step       Step   `json:"step"`
	Digest     string `json:"digest,omitempty"`
	Resolution string `json:"resolution,omitempty"`
}

// state is the persisted session. Original is the manifest as it stood before
// the first toggle and is retained until reset has restored and synced it, so
// an interrupted session never loses the manifest it must return to.
type state struct {
	Phase    Phase              `json:"phase"`
	Mods     []Mod              `json:"mods"`
	Original *document.Manifest `json:"original"`
	Left     int                `json:"left"`
	Right    int                `json:"right"`

	// Applied is the digest of the manifest the last successful transition
	// left in the workspace. Verdicts and reset refuse to overwrite a
	// manifest that no longer matches it, the pending target, or the
	// original.
	Applied string             `json:"applied,omitempty"`
	Pending *pendingTransition `json:"pending,omitempty"`
}

func stateFilePath(workDir string) string {
	return filepath.Join(workDir, stateFilename)
}

func lockFilePath(workDir string) string {
	return filepath.Join(workDir, ".lucy", lockFilename)
}

// statePresent checks for a session without creating workspace state.
func statePresent(workDir string) (bool, error) {
	_, err := os.Stat(stateFilePath(workDir))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, fmt.Errorf("inspect bisect state: %w", err)
}

// requireSession rejects a command that cannot have a session before the lock
// creates .lucy in a directory that is not a bisect workspace.
func requireSession(workDir string) error {
	present, err := statePresent(workDir)
	if err != nil {
		return err
	}
	if !present {
		return fmt.Errorf("no bisect session found, run `lucy bisect start` first")
	}
	return nil
}

// requireStartWorkspace rejects start before locking when there is neither a
// session to resume nor a manifest to capture. A lock-only workspace cannot
// start a bisect session because Original would be missing.
func requireStartWorkspace(workDir string) error {
	present, err := statePresent(workDir)
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

// lockSession serializes every session transition across processes. It waits
// instead of failing so a concurrent start, verdict, status, or reset observes
// the previous command's committed state, and it stops waiting when the
// command context ends so a canceled command cannot later mutate the session.
// require runs before .lucy is created, so a command that cannot have a
// session or manifest fails without leaving a workspace marker. It is
// separate from the workspace writer lock because installation sync takes that
// lock itself.
func lockSession(
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
	relative := filepath.Join(".lucy", lockFilename)
	if err := workspacewrite.SafePath(workDir, relative); err != nil {
		return nil, err
	}
	if err := fsutil.MkdirAllPrivate(filepath.Dir(lockFilePath(workDir))); err != nil {
		return nil, fmt.Errorf("create workspace state directory: %w", err)
	}
	if err := workspacewrite.SafePath(workDir, relative); err != nil {
		return nil, err
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		lock, err := fsutil.TryLockFile(lockFilePath(workDir))
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

// readState returns the session, or nil when this workspace has none. A
// corrupt state is an error rather than an empty session: replacing it would
// lose the manifest that reset must restore.
func readState(workDir string) (*state, error) {
	data, err := os.ReadFile(stateFilePath(workDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read bisect state: %w", err)
	}
	var session state
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf(
			"failed to parse bisect state: %w; the captured original, if present, remains in %s; extract its original value to restore lucy.yaml manually, then delete %s",
			err,
			stateFilename,
			stateFilename,
		)
	}
	if err := validateState(&session); err != nil {
		return nil, fmt.Errorf(
			"%w; the captured original, if present, remains in %s; extract its original value to restore lucy.yaml manually, then delete %s",
			err,
			stateFilename,
			stateFilename,
		)
	}
	return &session, nil
}

// loadSession reads the session and reports an actionable error when the
// workspace has none.
func loadSession(workDir string) (*state, error) {
	session, err := readState(workDir)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, fmt.Errorf("no bisect session found, run `lucy bisect start` first")
	}
	return session, nil
}

func stateBytes(session *state) ([]byte, error) {
	data, err := json.Marshal(session, jsontext.Multiline(true), jsontext.WithIndent("  "))
	if err != nil {
		return nil, fmt.Errorf("failed to serialize bisect state: %w", err)
	}
	return data, nil
}

func writeState(workDir string, session *state) error {
	if err := validateState(session); err != nil {
		return err
	}
	data, err := stateBytes(session)
	if err != nil {
		return err
	}
	if err := fsutil.AtomicWrite(stateFilePath(workDir), data, 0o600); err != nil {
		return fmt.Errorf("failed to write bisect state: %w", err)
	}
	return nil
}

// createState publishes the first state of a session without replacing one
// that another process may have created since the caller checked.
func createState(workDir string, session *state) (resultErr error) {
	if err := validateState(session); err != nil {
		return err
	}
	data, err := stateBytes(session)
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
		stateFilename,
		bytes.NewReader(data),
		fsutil.WriteOptions{Mode: 0o600},
	); err != nil {
		return fmt.Errorf("failed to create bisect state: %w", err)
	}
	return nil
}

func deleteState(workDir string) error {
	if err := os.Remove(stateFilePath(workDir)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove bisect state: %w", err)
	}
	return nil
}

func validateState(session *state) error {
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
		(session.Pending.Step != StepStart &&
			session.Pending.Step != StepReset) {
		if session.Applied == "" {
			return fmt.Errorf("invalid bisect state: no committed manifest digest")
		}
	} else if session.Pending.Step == StepStart && session.Applied != "" {
		return fmt.Errorf("invalid bisect state: interrupted start already has a committed manifest digest")
	}
	switch session.Phase {
	case PhaseActive:
		if session.Left < 0 || session.Right >= len(session.Mods) || session.Left > session.Right {
			return fmt.Errorf(
				"invalid bisect state: active range [%d, %d] outside %d packages",
				session.Left,
				session.Right,
				len(session.Mods),
			)
		}
	case PhaseFound:
		if session.Left != session.Right ||
			session.Left < 0 ||
			session.Left >= len(session.Mods) {
			return fmt.Errorf(
				"invalid bisect state: found range [%d, %d] is not one package",
				session.Left,
				session.Right,
			)
		}
	case PhaseExhausted:
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
	return validatePending(session)
}

func validatePending(session *state) error {
	pending := session.Pending
	if pending == nil {
		return nil
	}
	if pending.Digest == "" || pending.Resolution == "" {
		return fmt.Errorf("invalid bisect state: pending transition records no manifest digest and resolution fingerprint")
	}
	switch pending.Step {
	case StepStart:
		if session.Phase != PhaseActive ||
			session.Left != 0 ||
			session.Right != len(session.Mods)-1 {
			return fmt.Errorf("invalid bisect state: interrupted start does not own the full range")
		}
	case StepGood:
		if session.Left <= 0 ||
			(session.Phase != PhaseActive && session.Phase != PhaseExhausted) {
			return fmt.Errorf("invalid bisect state: interrupted good does not match its range or phase")
		}
	case StepBad:
		if session.Right < 0 ||
			(session.Phase != PhaseActive && session.Phase != PhaseFound) {
			return fmt.Errorf("invalid bisect state: interrupted bad does not match its range or phase")
		}
	case StepReset:
		// Reset can follow any phase; the pending digest names the manifest
		// the failed reset left in place.
	default:
		return fmt.Errorf("invalid bisect state: unknown pending step %q", pending.Step)
	}
	return nil
}

// errManifestChanged refuses to proceed when the manifest no longer matches
// any state the session itself published. Restoring the original would
// otherwise discard an unrelated edit without leaving a trace.
var errManifestChanged = errors.New(
	"lucy.yaml changed outside the bisect session; refusing to overwrite it. " +
		"Undo that change and retry, or recover the captured original from " +
		"bisect.json (mode 0600) and delete bisect.json to abandon the session",
)

var errManifestChangedDuringSync = errors.New(
	"lucy.yaml changed while the installation synced; the bisect transition " +
		"remains pending. Restore lucy.yaml and rerun the command, or recover " +
		"the captured original from bisect.json (mode 0600)",
)

// manifestResolutionFingerprint is the fingerprint installation uses to
// decide whether a lock still answers the manifest.
func manifestResolutionFingerprint(doc *document.Manifest) (string, error) {
	fingerprint, err := doc.Fingerprint()
	if err != nil {
		return "", fmt.Errorf("fingerprint manifest: %w", err)
	}
	return fingerprint, nil
}

// manifestDigest covers every persisted manifest field, including name
// metadata that resolution intentionally ignores. reset and verdicts use it to
// distinguish a manifest the session published from one edited outside it.
func manifestDigest(doc *document.Manifest) (string, error) {
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

// withWorkspace replays interrupted workspace journals under the shared
// writer lock and runs action while that lock is held. The caller must hold
// the bisect session lock; action must not call installation sync, which takes
// the same writer lock itself.
func withWorkspace(
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

type manifestRelation int

const (
	manifestForeign manifestRelation = iota
	manifestOwned
	// manifestRenamed means the manifest matches the pending target in every
	// field the resolution fingerprint covers, but differs in a field it
	// intentionally ignores, currently the server name. The caller adopts that
	// name instead of rejecting a rename that cannot affect installation.
	manifestRenamed
)

// classifyManifest reports how doc relates to the states this session
// published. Without a pending transition, a changed manifest is foreign even
// when only its name differs; that edit happened after a committed step and
// reset must not discard it silently.
func classifyManifest(
	session *state,
	doc *document.Manifest,
) (manifestRelation, error) {
	digest, err := manifestDigest(doc)
	if err != nil {
		return manifestForeign, err
	}
	if digest == session.Applied {
		return manifestOwned, nil
	}
	if session.Pending != nil && digest == session.Pending.Digest {
		return manifestOwned, nil
	}
	original, err := manifestDigest(session.Original)
	if err != nil {
		return manifestForeign, err
	}
	if digest == original {
		return manifestOwned, nil
	}
	if session.Pending == nil || session.Pending.Resolution == "" {
		return manifestForeign, nil
	}
	resolution, err := manifestResolutionFingerprint(doc)
	if err != nil {
		return manifestForeign, err
	}
	if resolution == session.Pending.Resolution {
		return manifestRenamed, nil
	}
	return manifestForeign, nil
}

func adoptRename(session *state, doc *document.Manifest) {
	session.Original.Name = doc.Name
}

// toggleRange enables requirements through mid and disables the rest after
// them without writing anything. It returns both the ownership digest and the
// resolution fingerprint machinery pins during sync.
func toggleRange(
	doc *document.Manifest,
	mods []Mod,
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
	digest, err = manifestDigest(doc)
	if err != nil {
		return 0, 0, 0, "", "", err
	}
	resolution, err = manifestResolutionFingerprint(doc)
	if err != nil {
		return 0, 0, 0, "", "", err
	}
	return enabled, disabled, matched, digest, resolution, nil
}

// prepareRangeLocked writes the target digest to the pending transition
// before changing the manifest, then writes the range. The caller holds the
// workspace writer lock after journal recovery.
func prepareRangeLocked(
	ctx context.Context,
	workDir string,
	session *state,
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
	relation, err := classifyManifest(session, doc)
	if err != nil {
		return 0, 0, "", "", err
	}
	switch relation {
	case manifestForeign:
		return 0, 0, "", "", errManifestChanged
	case manifestRenamed:
		adoptRename(session, doc)
	}
	mid := (session.Left + session.Right) / 2
	var matched int
	enabled, disabled, matched, digest, resolution, err = toggleRange(doc, session.Mods, mid)
	if err != nil {
		return 0, 0, "", "", err
	}
	session.Pending.Digest = digest
	session.Pending.Resolution = resolution
	if err := ctx.Err(); err != nil {
		return 0, 0, "", "", err
	}
	if err := writeState(workDir, session); err != nil {
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

// prepareRange stages the pending range. The caller syncs afterwards, once
// the workspace writer lock is released, because installation takes that lock
// itself.
func prepareRange(
	ctx context.Context,
	workDir string,
	logger *log.Logger,
	session *state,
) (enabled, disabled int, digest, resolution string, err error) {
	err = withWorkspace(ctx, workDir, logger, func() error {
		var actionErr error
		enabled, disabled, digest, resolution, actionErr = prepareRangeLocked(ctx, workDir, session)
		return actionErr
	})
	return enabled, disabled, digest, resolution, err
}

// setRequirementEnabled flips one manifest requirement and reports whether
// the entry was present.
func setRequirementEnabled(doc *document.Manifest, mod Mod, enabled bool) bool {
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

// writeOriginal restores the manifest captured before the first toggle. It
// records step and the target digest on the pending transition before
// writing, then writes the original. The caller syncs afterwards; a retry
// after a failed sync must run that sync even when the manifest already
// matches.
func writeOriginal(
	ctx context.Context,
	workDir string,
	logger *log.Logger,
	session *state,
	step Step,
) (digest, resolution string, err error) {
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	err = withWorkspace(ctx, workDir, logger, func() error {
		current, err := document.ManifestFile(workDir).Read()
		if err != nil {
			return fmt.Errorf("read manifest: %w", err)
		}
		relation, err := classifyManifest(session, current)
		if err != nil {
			return err
		}
		switch relation {
		case manifestForeign:
			return errManifestChanged
		case manifestRenamed:
			adoptRename(session, current)
		}
		originalDigest, err := manifestDigest(session.Original)
		if err != nil {
			return err
		}
		originalResolution, err := manifestResolutionFingerprint(session.Original)
		if err != nil {
			return err
		}
		digest = originalDigest
		resolution = originalResolution
		switch {
		case session.Pending == nil:
			session.Pending = &pendingTransition{
				Step:       step,
				Digest:     originalDigest,
				Resolution: originalResolution,
			}
		case step == StepReset:
			// Reset supersedes an interrupted leaf. Keep that leaf's target
			// digest until the original is written, so a retry can still
			// recognize the manifest the failed reset left in place.
			session.Pending.Step = StepReset
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
		if err := writeState(workDir, session); err != nil {
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
