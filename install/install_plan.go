package install

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/filetrack"
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

// checkPlannedDocuments verifies that a plan carries the documents it recorded
// as its base. Plans built from documents that journal recovery later replaced
// would otherwise publish state the recovered workspace never answered.
func checkPlannedDocuments(plan *Plan) error {
	manifestFingerprint := ""
	if plan.Manifest != nil {
		fingerprint, err := plan.Manifest.Fingerprint()
		if err != nil {
			return err
		}
		manifestFingerprint = fingerprint
	}
	if !plan.PublishManifest && manifestFingerprint != plan.BaseManifestFingerprint {
		return fmt.Errorf("plan manifest does not match its recorded base; replan from current intent")
	}
	lockFingerprint := ""
	if plan.Previous != nil {
		fingerprint, err := plan.Previous.Fingerprint()
		if err != nil {
			return err
		}
		lockFingerprint = fingerprint
	}
	if lockFingerprint != plan.BaseLockFingerprint {
		return fmt.Errorf("plan lock does not match its recorded base; replan from current state")
	}
	return nil
}

// checkPlannedState rejects a workspace whose documents no longer match the
// fingerprints the plan recorded. An empty fingerprint means the document was
// absent.
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
	currentManifest, currentLock, err := workspaceDocuments(root)
	if err != nil {
		return "", "", err
	}
	if currentManifest != nil {
		if manifestFingerprint, err = currentManifest.Fingerprint(); err != nil {
			return "", "", err
		}
	}
	if currentLock != nil {
		if lockFingerprint, err = currentLock.Fingerprint(); err != nil {
			return "", "", err
		}
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

// workspaceDocuments reads the documents a workspace currently holds. An
// absent document is nil, not an error.
func workspaceDocuments(root string) (*document.Manifest, *document.Lock, error) {
	currentManifest, err := document.ManifestFile(root).Read()
	switch {
	case errors.Is(err, os.ErrNotExist):
		currentManifest = nil
	case err != nil:
		return nil, nil, err
	}
	currentLock, err := document.LockFile(root).Read()
	switch {
	case errors.Is(err, os.ErrNotExist):
		currentLock = nil
	case err != nil:
		return nil, nil, err
	}
	return currentManifest, currentLock, nil
}

// recoveredLock prefers the caller's lock when it still describes the recovered
// lock, so a replay returns the document the caller already holds. A stale
// caller lock is replaced by the recovered one; a caller lock whose recovered
// counterpart disappeared is discarded.
func recoveredLock(caller, recovered *document.Lock) (*document.Lock, error) {
	if caller == nil || recovered == nil {
		return recovered, nil
	}
	callerFingerprint, err := caller.Fingerprint()
	if err != nil {
		return nil, err
	}
	recoveredFingerprint, err := recovered.Fingerprint()
	if err != nil {
		return nil, err
	}
	if callerFingerprint == recoveredFingerprint {
		return caller, nil
	}
	return recovered, nil
}

// preflightLocked replays interrupted journals before anything reads the
// documents they describe, then validates the state both planning and applying
// depend on. It returns the recovered documents, which are the only safe base
// for resolution and staging. The running-server check only applies when the
// operation will mutate the runtime tree.
func preflightLocked(
	root string,
	d *document.Manifest,
	old *document.Lock,
	opts Options,
	mutating bool,
	logger *log.Logger,
) (*document.Manifest, *document.Lock, error) {
	if d == nil && old == nil {
		return nil, nil, fmt.Errorf("lucy.yaml or lucy-lock.yaml is required")
	}
	// A pending file transaction is rolled back unless its event reached the
	// history. Reading, resolving, or publishing the manifest, lock, or tracked
	// files before that replay can invalidate the pre-images recovery needs.
	if err := filetrack.RecoverLocked(root, logger); err != nil {
		return nil, nil, err
	}
	baseManifest, baseLock, err := workspaceDocuments(root)
	if err != nil {
		return nil, nil, err
	}
	if mutating {
		// Recovery may restore a different runtime location than the caller
		// requested. Both the recovered locations and a manifest being
		// published must be stopped before staging rewrites their files.
		directories := make([]string, 0, 3)
		add := func(directory string) {
			if !slices.Contains(directories, directory) {
				directories = append(directories, directory)
			}
		}
		if baseManifest != nil {
			add(baseManifest.Server.Directory)
		}
		if baseLock != nil {
			add(baseLock.Server.Directory)
		}
		if opts.PublishManifest && d != nil {
			add(d.Server.Directory)
		}
		if baseManifest == nil && baseLock == nil {
			switch {
			case d != nil:
				add(d.Server.Directory)
			case old != nil:
				add(old.Server.Directory)
			}
		}
		for _, directory := range directories {
			if workspace.RunningAt(filepath.Join(root, directory)) ||
				(directory != "." && workspace.RunningAt(root)) {
				return nil, nil, fmt.Errorf("stop the running Minecraft server before installation")
			}
		}
	}
	// Callers that read the documents before the writer lock pin what they
	// read, so recovery or a concurrent edit fails instead of being overwritten.
	if opts.ExpectedManifest != "" {
		fingerprint := ""
		if baseManifest != nil {
			fingerprint, err = baseManifest.Fingerprint()
			if err != nil {
				return nil, nil, err
			}
		}
		if fingerprint != opts.ExpectedManifest {
			return nil, nil, fmt.Errorf("manifest changed during package selection; retry from current intent")
		}
	}
	if opts.ExpectedLock != "" {
		fingerprint := ""
		if baseLock != nil {
			fingerprint, err = baseLock.Fingerprint()
			if err != nil {
				return nil, nil, err
			}
		}
		if fingerprint != opts.ExpectedLock {
			return nil, nil, fmt.Errorf("lock changed during package selection; retry from current state")
		}
	}
	return baseManifest, baseLock, nil
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
	opts Options,
	result *Result,
) *downloadStore {
	return &downloadStore{
		ctx:         ctx,
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
