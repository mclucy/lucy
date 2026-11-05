package bisect

import (
	"context"
	"errors"
	"fmt"

	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/operation"
	"github.com/mclucy/lucy/workspace/identify"
)

// Start begins a session, or completes an interrupted start. It captures the
// manifest, selects the requirements to search, and syncs the first range.
// Findings from candidate selection are returned even when the transition
// later fails.
func Start(
	ctx context.Context,
	workDir string,
	logger *log.Logger,
	ops *operation.Service,
	lookup identify.HashLookup,
) (result Result, resultErr error) {
	lock, err := lockSession(ctx, workDir, requireStartWorkspace)
	if err != nil {
		return Result{}, err
	}
	defer func() { resultErr = errors.Join(resultErr, lock.Close()) }()
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	existing, err := readState(workDir)
	if err != nil {
		return Result{}, err
	}
	if existing != nil {
		if existing.Pending != nil && existing.Pending.Step == StepStart {
			return completeTransition(ctx, ops, workDir, logger, existing)
		}
		return Result{}, startRefusal(existing)
	}

	var (
		original       *document.Manifest
		originalDigest string
	)
	if err := withWorkspace(ctx, workDir, logger, func() error {
		doc, err := document.ManifestFile(workDir).Read()
		if err != nil {
			return fmt.Errorf("read manifest: %w", err)
		}
		original = doc
		originalDigest, err = manifestDigest(doc)
		return err
	}); err != nil {
		return Result{}, err
	}

	mods, findings, err := selectCandidates(ctx, lookup, workDir, original)
	result = Result{Findings: findings}
	if err != nil {
		return result, err
	}
	if len(mods) == 0 {
		return Result{
			Complete:     true,
			Findings:     findings,
			NoCandidates: true,
		}, nil
	}

	session := &state{
		Phase:    PhaseActive,
		Mods:     mods,
		Original: original,
		Right:    len(mods) - 1,
		Pending:  &pendingTransition{Step: StepStart},
	}
	var (
		enabled          int
		disabled         int
		matched          int
		targetDigest     string
		targetResolution string
	)
	if err := withWorkspace(ctx, workDir, logger, func() error {
		current, err := document.ManifestFile(workDir).Read()
		if err != nil {
			return fmt.Errorf("read manifest: %w", err)
		}
		currentDigest, err := manifestDigest(current)
		if err != nil {
			return err
		}
		if currentDigest != originalDigest {
			return fmt.Errorf(
				"manifest changed during package selection; retry `lucy bisect start`",
			)
		}
		mid := (session.Left + session.Right) / 2
		enabled, disabled, matched, targetDigest, targetResolution, err = toggleRange(current, session.Mods, mid)
		if err != nil {
			return err
		}
		session.Pending.Digest = targetDigest
		session.Pending.Resolution = targetResolution
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := createState(workDir, session); err != nil {
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
		return result, err
	}

	output, err := commitTransition(
		ctx,
		ops,
		workDir,
		logger,
		session,
		*session.Pending,
		targetDigest,
		targetResolution,
		enabled,
		disabled,
		0,
	)
	if err != nil {
		return result, err
	}
	output.Findings = findings
	return output, nil
}

// Good advances past the midpoint because the tested requirement works.
func Good(
	ctx context.Context,
	workDir string,
	logger *log.Logger,
	ops *operation.Service,
) (result Result, resultErr error) {
	lock, err := lockSession(ctx, workDir, requireSession)
	if err != nil {
		return Result{}, err
	}
	defer func() { resultErr = errors.Join(resultErr, lock.Close()) }()
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	session, err := loadSession(workDir)
	if err != nil {
		return Result{}, err
	}
	if session.Pending != nil {
		if session.Pending.Step != StepGood {
			return Result{}, interruptedError(session.Pending)
		}
		return completeTransition(ctx, ops, workDir, logger, session)
	}
	if err := terminalError(session); err != nil {
		return Result{}, err
	}

	mid := (session.Left + session.Right) / 2
	session.Left = mid + 1
	session.Pending = &pendingTransition{Step: StepGood}
	if session.Left > session.Right {
		session.Phase = PhaseExhausted
	}
	return completeTransition(ctx, ops, workDir, logger, session)
}

// Bad moves the range end onto the midpoint because the tested requirement
// fails.
func Bad(
	ctx context.Context,
	workDir string,
	logger *log.Logger,
	ops *operation.Service,
) (result Result, resultErr error) {
	lock, err := lockSession(ctx, workDir, requireSession)
	if err != nil {
		return Result{}, err
	}
	defer func() { resultErr = errors.Join(resultErr, lock.Close()) }()
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	session, err := loadSession(workDir)
	if err != nil {
		return Result{}, err
	}
	if session.Pending != nil {
		if session.Pending.Step != StepBad {
			return Result{}, interruptedError(session.Pending)
		}
		return completeTransition(ctx, ops, workDir, logger, session)
	}
	if err := terminalError(session); err != nil {
		return Result{}, err
	}

	mid := (session.Left + session.Right) / 2
	session.Right = mid
	session.Pending = &pendingTransition{Step: StepBad}
	if session.Left == session.Right {
		session.Phase = PhaseFound
	}
	return completeTransition(ctx, ops, workDir, logger, session)
}

// Status reads the session without changing it. A pending transition is
// reported as the step that must be rerun.
func Status(ctx context.Context, workDir string) (result Result, resultErr error) {
	lock, err := lockSession(ctx, workDir, requireSession)
	if err != nil {
		return Result{}, err
	}
	defer func() { resultErr = errors.Join(resultErr, lock.Close()) }()
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	session, err := loadSession(workDir)
	if err != nil {
		return Result{}, err
	}

	result = Result{
		Phase:    session.Phase,
		Complete: session.Phase != PhaseActive,
		State:    currentView(session),
	}
	if session.Phase == PhaseFound {
		result.Found = &session.Mods[session.Left]
	}
	if session.Pending != nil {
		result.Pending = session.Pending.Step
	}
	return result, nil
}

// Reset restores the captured manifest, syncs the installation, and removes
// the session. The session survives a failed restore or sync so the command
// can be retried.
func Reset(
	ctx context.Context,
	workDir string,
	logger *log.Logger,
	ops *operation.Service,
) (result Result, resultErr error) {
	lock, err := lockSession(ctx, workDir, requireSession)
	if err != nil {
		return Result{}, err
	}
	defer func() { resultErr = errors.Join(resultErr, lock.Close()) }()
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	session, err := loadSession(workDir)
	if err != nil {
		return Result{}, err
	}
	// Reset supersedes any pending transition: restoring the captured
	// manifest and syncing it is the final state, whatever the interrupted
	// leaf had intended. The session file is kept until this succeeds, so a
	// failed restore remains retryable.
	_, targetResolution, err := writeOriginal(
		ctx,
		workDir,
		logger,
		session,
		StepReset,
	)
	if err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	syncResult, err := ops.Sync(
		ctx,
		workDir,
		install.Options{ExpectedManifest: targetResolution},
	)
	if err != nil {
		return Result{}, fmt.Errorf("sync installation: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if err := deleteState(workDir); err != nil {
		return Result{}, err
	}

	return Result{
		Complete: true,
		Restored: countRequirements(session.Original),
		Warnings: syncWarnings(syncResult),
	}, nil
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

func currentView(session *state) *View {
	view := &View{
		Total: len(session.Mods),
		Left:  session.Left,
		Right: session.Right,
	}
	if session.Phase != PhaseActive {
		return view
	}
	view.Remaining = session.Right - session.Left + 1
	mid := (session.Left + session.Right) / 2
	view.Midpoint = mid
	view.Candidate = &session.Mods[mid]
	return view
}

// completeTransition writes the manifest target recorded by a pending
// transition, syncs the installation, and only then clears the pending record.
// The record is durable before the first manifest write, so a failure at any
// point leaves a state that the same leaf can resume.
func completeTransition(
	ctx context.Context,
	ops *operation.Service,
	workDir string,
	logger *log.Logger,
	session *state,
) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	pending := session.Pending
	if pending == nil {
		return Result{}, fmt.Errorf("invalid bisect state: no pending transition")
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
	case PhaseActive:
		enabled, disabled, targetDigest, targetResolution, err = prepareRange(ctx, workDir, logger, session)
	case PhaseFound, PhaseExhausted:
		targetDigest, targetResolution, err = writeOriginal(ctx, workDir, logger, session, pending.Step)
		restored = countRequirements(session.Original)
	default:
		return Result{}, fmt.Errorf("invalid bisect state: unknown phase %q", session.Phase)
	}
	if err != nil {
		return Result{}, err
	}
	return commitTransition(
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

// commitTransition syncs the staged manifest, then records the committed
// digest and clears the pending transition. A failure before the final write
// leaves both the staged manifest and the pending record for a retry.
func commitTransition(
	ctx context.Context,
	ops *operation.Service,
	workDir string,
	logger *log.Logger,
	session *state,
	pending pendingTransition,
	targetDigest string,
	targetResolution string,
	enabled, disabled, restored int,
) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	// Sync even when the manifest already shows the target: the manifest
	// states what should be installed, not what is installed. ExpectedManifest
	// also makes a concurrent edit fail instead of being overwritten by
	// whichever document the sync happened to read.
	syncResult, err := ops.Sync(
		ctx,
		workDir,
		install.Options{ExpectedManifest: targetResolution},
	)
	if err != nil {
		return Result{}, fmt.Errorf("sync installation: %w", err)
	}
	// The resolution fingerprint intentionally ignores name metadata, so sync
	// can succeed after a concurrent rename. Validate the full digest before
	// clearing pending; otherwise the session would record a stale Applied
	// digest and reject every later command.
	applied := targetDigest
	err = withWorkspace(ctx, workDir, logger, func() error {
		current, err := document.ManifestFile(workDir).Read()
		if err != nil {
			return fmt.Errorf("read manifest after sync: %w", err)
		}
		currentDigest, err := manifestDigest(current)
		if err != nil {
			return err
		}
		if currentDigest != targetDigest {
			currentResolution, err := manifestResolutionFingerprint(current)
			if err != nil {
				return err
			}
			if currentResolution != targetResolution {
				return errManifestChangedDuringSync
			}
			// Only fields resolution ignores changed, currently the server
			// name. Adopt the rename into the original so reset preserves it
			// instead of reverting a change the session already observed.
			adoptRename(session, current)
			applied = currentDigest
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		session.Pending = nil
		session.Applied = applied
		return writeState(workDir, session)
	})
	if err != nil {
		return Result{}, err
	}
	output := Result{
		Phase:    session.Phase,
		Complete: session.Phase != PhaseActive,
		State:    currentView(session),
		Enabled:  enabled,
		Disabled: disabled,
		Restored: restored,
		Warnings: syncWarnings(syncResult),
		Marked:   markedMod(session, pending.Step),
	}
	if session.Phase == PhaseFound {
		output.Found = &session.Mods[session.Left]
	}
	return output, nil
}

// markedMod reports the requirement a completed step marked.
func markedMod(session *state, step Step) *Mod {
	switch step {
	case StepGood:
		if session.Left > 0 && session.Left <= len(session.Mods) {
			return &session.Mods[session.Left-1]
		}
	case StepBad:
		if session.Right >= 0 && session.Right < len(session.Mods) {
			return &session.Mods[session.Right]
		}
	}
	return nil
}

// interruptedError refuses a different leaf while a transition is pending.
// The target range already contains the recorded verdict, so letting another
// verdict advance it again would corrupt the search.
func interruptedError(pending *pendingTransition) error {
	if pending.Step == StepReset {
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

// terminalError refuses a new verdict after a session reached its terminal
// phase. Only reset may end the session, so the recorded verdict cannot
// silently change.
func terminalError(session *state) error {
	switch session.Phase {
	case PhaseFound:
		return fmt.Errorf(
			"bisect session already found %s; run `lucy bisect reset` before another verdict",
			session.Mods[session.Left].Label(),
		)
	case PhaseExhausted:
		return fmt.Errorf("bisect session already finished with no bad package; run `lucy bisect reset` before another verdict")
	default:
		return nil
	}
}

// startRefusal explains why start cannot replace an existing session. The
// captured manifest lives only in the session file, so replacing that file
// would make the original unrecoverable.
func startRefusal(session *state) error {
	if session.Pending != nil {
		if session.Pending.Step == StepReset {
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
	case PhaseFound:
		return fmt.Errorf(
			"bisect session already found %s; run `lucy bisect reset` before starting another session",
			session.Mods[session.Left].Label(),
		)
	case PhaseExhausted:
		return fmt.Errorf("bisect session already finished with no bad package; run `lucy bisect reset` before starting another session")
	default:
		return fmt.Errorf("bisect session already active; run `lucy bisect reset` before starting another session")
	}
}
