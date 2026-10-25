package install

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/lockfile"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/manifest"
	"github.com/mclucy/lucy/workspace"
)

// Plan is the resolved outcome of one installation decision. It carries the
// documents the commit will publish and the fingerprints it was planned from,
// so applying it cannot silently consume a concurrent edit.
type Plan struct {
	Manifest        *manifest.Document
	Previous        *lockfile.Document
	Resolved        *lockfile.Document
	PublishManifest bool
	PublishLock     bool
	// BaseManifestFingerprint and BaseLockFingerprint describe the workspace
	// state the plan answers. ApplyPlan rejects a workspace that no longer
	// matches them.
	BaseManifestFingerprint string
	BaseLockFingerprint     string
	Result                  *Result
}

// PlanInstall resolves a lock without staging or committing the server tree.
// It holds the workspace writer lock only while reading and resolving.
func PlanInstall(
	ctx context.Context,
	root string,
	d *manifest.Document,
	old *lockfile.Document,
	opts Options,
) (plan *Plan, resultErr error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	root = absolute
	if err := workspacewrite.SafePath(root, ".lucy"); err != nil {
		return nil, err
	}
	writer, err := workspacewrite.Lock(root)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, writer.Close()) }()
	if err := preflightLocked(root, d, old, opts, false); err != nil {
		return nil, err
	}
	result := newResult()
	store := newDownloadStore(ctx, root, opts, result)
	resolved, err := resolveLock(ctx, root, d, old, &opts, store, result)
	if err != nil {
		return nil, err
	}
	result.PublishedManifest = opts.PublishManifest
	result.PublishedLock = opts.publishLock
	baseManifest, baseLock, err := workspaceFingerprints(root)
	if err != nil {
		return nil, err
	}
	return &Plan{
		Manifest:                d,
		Previous:                old,
		Resolved:                resolved,
		PublishManifest:         opts.PublishManifest,
		PublishLock:             opts.publishLock,
		BaseManifestFingerprint: baseManifest,
		BaseLockFingerprint:     baseLock,
		Result:                  result,
	}, nil
}

// ApplyPlan stages and commits a previously resolved plan. It rechecks the
// workspace state before staging, so a plan cannot overwrite a concurrent
// manifest or lock edit.
func ApplyPlan(
	ctx context.Context,
	root string,
	plan *Plan,
	opts Options,
) (result *Result, resultErr error) {
	if plan == nil || plan.Resolved == nil {
		return nil, fmt.Errorf("installation plan is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	root = absolute
	if err := workspacewrite.SafePath(root, ".lucy"); err != nil {
		return nil, err
	}
	writer, err := workspacewrite.Lock(root)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, writer.Close()) }()
	if err := preflightLocked(root, plan.Manifest, plan.Previous, opts, true); err != nil {
		return nil, err
	}
	if err := checkPlannedState(root, plan); err != nil {
		return nil, err
	}
	result = plan.Result
	if result == nil {
		result = newResult()
		result.applyChanges(plan.Previous, plan.Resolved, plan.Manifest)
	}
	store := newDownloadStore(ctx, root, opts, result)
	opts.PublishManifest = plan.PublishManifest
	opts.publishLock = plan.PublishLock
	diagnostics, err := stageInstall(ctx, root, plan.Manifest, plan.Resolved, store, opts, result)
	result.Diagnostics = diagnostics
	return result, err
}

func checkPlannedState(root string, plan *Plan) error {
	manifestFingerprint, lockFingerprint, err := workspaceFingerprints(root)
	if err != nil {
		return err
	}
	if err := comparePlannedFingerprint(
		"manifest",
		plan.BaseManifestFingerprint,
		manifestFingerprint,
		"intent",
	); err != nil {
		return err
	}
	return comparePlannedFingerprint(
		"lock",
		plan.BaseLockFingerprint,
		lockFingerprint,
		"state",
	)
}

// workspaceFingerprints reports the fingerprints of the manifest and lock a
// workspace currently holds. An absent document yields an empty fingerprint,
// so planning and applying share one description of the state a plan answers.
func workspaceFingerprints(root string) (manifestFingerprint, lockFingerprint string, err error) {
	currentManifest, err := manifest.Read(root)
	switch {
	case err == nil:
		if manifestFingerprint, err = manifest.Fingerprint(currentManifest); err != nil {
			return "", "", err
		}
	case !errors.Is(err, os.ErrNotExist):
		return "", "", err
	}
	currentLock, err := lockfile.Read(root)
	switch {
	case err == nil:
		if lockFingerprint, err = lockfile.Fingerprint(currentLock); err != nil {
			return "", "", err
		}
	case !errors.Is(err, os.ErrNotExist):
		return "", "", err
	}
	return manifestFingerprint, lockFingerprint, nil
}

// comparePlannedFingerprint reports how a workspace document drifted from the
// state the plan recorded. An empty fingerprint means the document was absent.
func comparePlannedFingerprint(name, planned, current, retry string) error {
	switch {
	case planned == current:
		return nil
	case current == "":
		return fmt.Errorf("%s disappeared after planning; retry from current %s", name, retry)
	case planned == "":
		return fmt.Errorf("%s appeared after planning; retry from current %s", name, retry)
	default:
		return fmt.Errorf("%s changed after planning; retry from current %s", name, retry)
	}
}

// preflightLocked validates the state both planning and applying depend on.
// The running-server check only applies when the operation will mutate the
// runtime tree.
func preflightLocked(
	root string,
	d *manifest.Document,
	old *lockfile.Document,
	opts Options,
	mutating bool,
) error {
	if d == nil && old == nil {
		return fmt.Errorf("lucy.yaml or lucy-lock.yaml is required")
	}
	if err := workspacewrite.RecoverInstall(root); err != nil {
		return err
	}
	if mutating {
		serverDirectory := ""
		if d != nil {
			serverDirectory = d.Server.Directory
		} else if old != nil {
			serverDirectory = old.Server.Directory
		}
		if workspace.RunningAt(filepath.Join(root, serverDirectory)) ||
			(serverDirectory != "." && workspace.RunningAt(root)) {
			return fmt.Errorf("stop the running Minecraft server before installation")
		}
		if err := workspacewrite.RecoverInstall(root); err != nil {
			return err
		}
	}
	if opts.ExpectedManifest != "" {
		current, err := manifest.Read(root)
		if err != nil {
			return err
		}
		fingerprint, err := manifest.Fingerprint(current)
		if err != nil {
			return err
		}
		if fingerprint != opts.ExpectedManifest {
			return fmt.Errorf("manifest changed during package selection; retry from current intent")
		}
	}
	if opts.ExpectedLock != "" {
		if old == nil {
			return fmt.Errorf("expected lock fingerprint but no lock was read")
		}
		fingerprint, err := lockfile.Fingerprint(old)
		if err != nil {
			return err
		}
		if fingerprint != opts.ExpectedLock {
			return fmt.Errorf("lock changed during package selection; retry from current state")
		}
	}
	return nil
}

// resolveLock performs the installation mode decision and resolution. The
// caller must hold the workspace writer lock.
func resolveLock(
	ctx context.Context,
	root string,
	d *manifest.Document,
	old *lockfile.Document,
	opts *Options,
	store *downloadStore,
	result *Result,
) (*lockfile.Document, error) {
	resolved := old
	fingerprint := ""
	if d != nil {
		var err error
		fingerprint, err = manifest.Fingerprint(d)
		if err != nil {
			return nil, err
		}
	}
	resolveNeeded := false
	switch {
	case opts.ForceResolve:
		result.Mode = ModeResolve
		result.Reason = "forced resolution"
		resolveNeeded = true
	case old == nil:
		result.Mode = ModeResolve
		result.Reason = "no lock file to replay"
		resolveNeeded = true
	case opts.WithOptional:
		result.Mode = ModeResolve
		result.Reason = "optional packages requested"
		resolveNeeded = true
	case d != nil && old.ManifestFingerprint != fingerprint:
		result.Mode = ModeResolve
		result.Reason = "manifest changed since lock"
		resolveNeeded = true
	default:
		result.Mode = ModeReplay
		result.Reason = "manifest unchanged; lock replays exactly"
	}
	log.Debug(
		"installation decision",
		"mode", string(result.Mode),
		"reason", result.Reason,
	)
	if resolveNeeded {
		if opts.Locked {
			if opts.ForceResolve {
				return nil, fmt.Errorf("locked installation cannot force resolution")
			}
			if !opts.RequirementChange || old == nil || d == nil {
				return nil, fmt.Errorf("locked installation requires a current lucy-lock.yaml")
			}
			reusable, err := coreReusable(ctx, d, old, opts.Offline)
			if err != nil {
				return nil, err
			}
			if !reusable {
				return nil, fmt.Errorf("manifest core no longer matches lucy-lock.yaml; install without --locked")
			}
			log.Debug(
				"locked installation reuses core",
				"reason", "manifest core still matches lock; requirements rebind in place",
			)
			resolved, err = mutateLock(d, old, fingerprint)
			if err != nil {
				return nil, err
			}
			if err := resolved.Validate(); err != nil {
				return nil, err
			}
			result.Mode = ModeLocked
			result.Reason = "manifest core matches; requirements rebind in place"
			opts.publishLock = true
			result.applyChanges(old, resolved, d)
			return resolved, nil
		}
		var err error
		resolved, err = resolveInstall(ctx, root, d, old, fingerprint, opts, store, result)
		if err != nil {
			return nil, err
		}
	}
	if err := resolved.Validate(); err != nil {
		return nil, err
	}
	opts.publishLock = resolved != old
	result.applyChanges(old, resolved, d)
	return resolved, nil
}

func newDownloadStore(
	ctx context.Context,
	root string,
	opts Options,
	result *Result,
) *downloadStore {
	return &downloadStore{
		ctx:     ctx,
		dir:     filepath.Join(root, ".lucy", "downloads"),
		offline: opts.Offline,
		trace:   opts.Trace,
		fetch:   &result.Fetch,
	}
}
