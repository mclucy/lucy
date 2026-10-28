package install

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/workspace"
)

// Plan is the resolved outcome of one installation decision. It carries the
// documents the commit will publish and the fingerprints it was planned from,
// so applying it cannot silently consume a concurrent edit.
type Plan struct {
	Manifest        *document.Manifest
	Previous        *document.Lock
	Resolved        *document.Lock
	PublishManifest bool
	PublishLock     bool
	// BaseManifestFingerprint and BaseLockFingerprint describe the workspace
	// state the plan answers. ApplyPlan rejects a workspace that no longer
	// matches them.
	BaseManifestFingerprint string
	BaseLockFingerprint     string
	Result                  *Result
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
	currentManifest, err := document.ManifestFile(root).Read()
	switch {
	case err == nil:
		if manifestFingerprint, err = currentManifest.Fingerprint(); err != nil {
			return "", "", err
		}
	case !errors.Is(err, os.ErrNotExist):
		return "", "", err
	}
	currentLock, err := document.LockFile(root).Read()
	switch {
	case err == nil:
		if lockFingerprint, err = currentLock.Fingerprint(); err != nil {
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
	d *document.Manifest,
	old *document.Lock,
	opts Options,
	mutating bool,
	logger *log.Logger,
) error {
	if d == nil && old == nil {
		return fmt.Errorf("lucy.yaml or lucy-lock.yaml is required")
	}
	if err := workspacewrite.RecoverInstall(root, logger); err != nil {
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
		if err := workspacewrite.RecoverInstall(root, logger); err != nil {
			return err
		}
	}
	if opts.ExpectedManifest != "" {
		current, err := document.ManifestFile(root).Read()
		if err != nil {
			return err
		}
		fingerprint, err := current.Fingerprint()
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
		fingerprint, err := old.Fingerprint()
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
func (s *Service) resolveLock(
	ctx context.Context,
	root string,
	d *document.Manifest,
	old *document.Lock,
	opts *Options,
	store *downloadStore,
	result *Result,
) (*document.Lock, error) {
	resolved := old
	fingerprint := ""
	if d != nil {
		var err error
		fingerprint, err = d.Fingerprint()
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
	s.logger.Debug(
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
			reusable, err := s.coreReusable(ctx, d, old, opts.Offline)
			if err != nil {
				return nil, err
			}
			if !reusable {
				return nil, fmt.Errorf("manifest core no longer matches lucy-lock.yaml; install without --locked")
			}
			s.logger.Debug(
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
		resolved, err = s.resolveInstall(ctx, root, d, old, fingerprint, opts, store, result)
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

func (s *Service) newDownloadStore(
	ctx context.Context,
	root string,
	opts Options,
	result *Result,
) *downloadStore {
	return &downloadStore{
		ctx:         ctx,
		dir:         filepath.Join(root, ".lucy", "downloads"),
		offline:     opts.Offline,
		trace:       opts.Trace,
		fetch:       &result.Fetch,
		network:     s.network,
		logger:      s.logger,
		githubProxy: s.githubProxy,
		injected:    opts.Materialize,
	}
}

// PlanFromResolution builds a plan from a lock another resolver produced. The
// caller must set the final manifest and the lock's manifest fingerprint, so a
// plan can never publish a lock that answers a different intent.
func PlanFromResolution(
	d *document.Manifest,
	previous *document.Lock,
	resolved *document.Lock,
	baseManifestFingerprint string,
	publishManifest bool,
) (*Plan, error) {
	if d == nil || resolved == nil {
		return nil, fmt.Errorf("manifest and resolved lock are required")
	}
	fingerprint, err := d.Fingerprint()
	if err != nil {
		return nil, err
	}
	if resolved.ManifestFingerprint != fingerprint {
		return nil, fmt.Errorf("resolved lock answers a different manifest")
	}
	if err := resolved.Validate(); err != nil {
		return nil, err
	}
	baseLock := ""
	if previous != nil {
		baseLock, err = previous.Fingerprint()
		if err != nil {
			return nil, err
		}
	}
	result := newResult()
	result.Kind = ResultKindUpgrade
	result.Mode = ModeResolve
	result.Reason = "package upgrade"
	result.applyChanges(previous, resolved, d)
	result.PublishedManifest = publishManifest
	result.PublishedLock = previous == nil || resolved != previous
	return &Plan{
		Manifest:                d,
		Previous:                previous,
		Resolved:                resolved,
		PublishManifest:         publishManifest,
		PublishLock:             result.PublishedLock,
		BaseManifestFingerprint: baseManifestFingerprint,
		BaseLockFingerprint:     baseLock,
		Result:                  result,
	}, nil
}
