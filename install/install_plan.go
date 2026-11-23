package install

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/filetrack"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/workspace"
)

// Plan is the resolved outcome of one installation decision. It carries the
// documents the commit will publish and the exact workspace capture it was
// planned from, so applying it cannot silently consume a concurrent edit.
type Plan struct {
	Manifest        *document.Manifest
	Previous        *document.Lock
	Resolved        *document.Lock
	PublishManifest bool
	PublishLock     bool
	// base pins the manifest, lock, and receipt bytes this plan answers.
	// ApplyPlan refuses a workspace whose captured documents differ.
	base   WorkspaceDocuments
	Result *Result
}

// checkBase verifies the workspace still holds the documents the plan was
// built from, and that the documents the plan answers agree with that capture.
// The decoded manifest and lock are the preflight observation of that same
// capture, so no extra fingerprint data is stored on the plan.
func (p *Plan) checkBase(
	manifest *document.Manifest,
	lock *document.Lock,
	documents WorkspaceDocuments,
) error {
	if len(p.base.documents) == 0 {
		return fmt.Errorf("plan has no captured workspace state; replan from current documents")
	}
	if err := p.base.matches(documents); err != nil {
		return err
	}
	if !p.PublishManifest {
		planned, err := p.Manifest.Fingerprint()
		if err != nil {
			return err
		}
		captured := ""
		if manifest != nil {
			if captured, err = manifest.Fingerprint(); err != nil {
				return err
			}
		}
		if planned != captured {
			return fmt.Errorf("plan manifest does not answer the captured workspace; replan from current intent")
		}
	}
	planned, captured := "", ""
	var err error
	if p.Previous != nil {
		if planned, err = p.Previous.Fingerprint(); err != nil {
			return err
		}
	}
	if lock != nil {
		if captured, err = lock.Fingerprint(); err != nil {
			return err
		}
	}
	if planned != captured {
		return fmt.Errorf("plan previous lock does not answer the captured workspace; replan from current state")
	}
	return nil
}

// Decode reads the manifest and lock pinned by a capture. An absent document
// is nil, not an error.
func (d WorkspaceDocuments) Decode() (*document.Manifest, *document.Lock, error) {
	var manifest *document.Manifest
	if pin := d.documents[document.ManifestFilename]; pin.image != nil {
		var err error
		if manifest, err = document.DecodeManifest(bytes.NewReader(pin.raw)); err != nil {
			return nil, nil, err
		}
	}
	lock, err := d.DecodeLock()
	if err != nil {
		return nil, nil, err
	}
	return manifest, lock, nil
}

// DecodeLock reads the captured lock without decoding the manifest. A capture
// without a lock returns nil.
func (d WorkspaceDocuments) DecodeLock() (*document.Lock, error) {
	pin := d.documents[document.LockFilename]
	if pin.image == nil {
		return nil, nil
	}
	return document.DecodeLock(bytes.NewReader(pin.raw))
}

// matches reports whether current holds the same document images d pinned.
func (d WorkspaceDocuments) matches(current WorkspaceDocuments) error {
	for _, name := range workspaceDocumentNames {
		if !workspacewrite.InstallImagesEqual(current.documents[name].image, d.documents[name].image) {
			return fmt.Errorf("workspace document %s differs from the plan base; replan from current state", name)
		}
	}
	return nil
}

// preflightLocked replays interrupted journals before anything reads the
// documents they describe, then validates the state both planning and applying
// depend on. It returns the recovered documents and the exact bytes observed
// after recovery, which are the only safe base for resolution and staging.
// Because every caller mutates the runtime tree, it also refuses to continue
// while a server still runs there.
func preflightLocked(
	root string,
	d *document.Manifest,
	old *document.Lock,
	opts Options,
	logger *log.Logger,
) (*document.Manifest, *document.Lock, WorkspaceDocuments, error) {
	if d == nil && old == nil {
		return nil, nil, WorkspaceDocuments{}, fmt.Errorf("lucy.yaml or lucy-lock.yaml is required")
	}
	// A pending file transaction is rolled back unless its event reached the
	// history. Reading, resolving, or publishing the manifest, lock, or tracked
	// files before that replay can invalidate the pre-images recovery needs.
	if err := filetrack.RecoverLocked(root, logger); err != nil {
		return nil, nil, WorkspaceDocuments{}, err
	}
	documents, err := CaptureWorkspaceDocuments(root)
	if err != nil {
		return nil, nil, WorkspaceDocuments{}, err
	}
	baseManifest, baseLock, err := documents.Decode()
	if err != nil {
		return nil, nil, WorkspaceDocuments{}, err
	}
	// Recovery may restore a different runtime location than the caller
	// requested. Both the recovered locations and a manifest being published
	// must be stopped before staging rewrites their files.
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
			return nil, nil, WorkspaceDocuments{}, fmt.Errorf("stop the running Minecraft server before installation")
		}
	}
	// Callers that read the documents before the writer lock pin what they
	// read, so recovery or a concurrent edit fails instead of being overwritten.
	if opts.ExpectedManifest != "" {
		fingerprint := ""
		if baseManifest != nil {
			fingerprint, err = baseManifest.Fingerprint()
			if err != nil {
				return nil, nil, WorkspaceDocuments{}, err
			}
		}
		if fingerprint != opts.ExpectedManifest {
			return nil, nil, WorkspaceDocuments{}, fmt.Errorf("manifest changed during package selection; retry from current intent")
		}
	}
	if opts.ExpectedLock != "" {
		fingerprint := ""
		if baseLock != nil {
			fingerprint, err = baseLock.Fingerprint()
			if err != nil {
				return nil, nil, WorkspaceDocuments{}, err
			}
		}
		if fingerprint != opts.ExpectedLock {
			return nil, nil, WorkspaceDocuments{}, fmt.Errorf("lock changed during package selection; retry from current state")
		}
	}
	return baseManifest, baseLock, documents, nil
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

func (s *Service) newDownloadStore(opts Options, result *Result) *downloadStore {
	return &downloadStore{
		offline:     opts.Offline,
		trace:       opts.Trace,
		fetch:       &result.Fetch,
		network:     s.network,
		logger:      s.logger,
		githubProxy: s.githubProxy,
		injected:    opts.Materialize,
	}
}

// PlanFromResolution builds a plan from a lock another resolver produced. base
// must be the workspace capture the plan answers; ApplyPlan compares it with
// the exact documents it later observes. The caller must set the final manifest
// and the lock's manifest fingerprint, so a plan can never publish a lock that
// answers a different intent.
func PlanFromResolution(
	base WorkspaceDocuments,
	d *document.Manifest,
	previous, resolved *document.Lock,
	publishManifest bool,
) (*Plan, error) {
	if len(base.documents) == 0 {
		return nil, fmt.Errorf("plan requires the workspace capture it answers")
	}
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
	result := newResult()
	result.Kind = ResultKindUpgrade
	result.Mode = ModeResolve
	result.Reason = "package upgrade"
	result.applyChanges(previous, resolved, d)
	result.PublishedManifest = publishManifest
	result.PublishedLock = previous == nil || resolved != previous
	return &Plan{
		Manifest:        d,
		Previous:        previous,
		Resolved:        resolved,
		PublishManifest: publishManifest,
		PublishLock:     result.PublishedLock,
		base:            base,
		Result:          result,
	}, nil
}
