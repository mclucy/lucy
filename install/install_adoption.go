package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/receipt"
	"github.com/mclucy/lucy/workspace/discovery"
)

// AdoptedPackage identifies a package file discovered during initialization.
type AdoptedPackage struct {
	Path           string
	PackageID      string
	ExpectedSHA256 string
	ExpectedMode   uint32
}

// AdoptedRuntimeFile identifies a runtime file discovered during initialization.
type AdoptedRuntimeFile struct {
	Path           string
	Role           discovery.FileRole
	ExpectedSHA256 string
	ExpectedMode   uint32
}

// AdoptOptions controls explicit initialization adoption.
type AdoptOptions struct {
	Manifest     *document.Manifest
	Lock         *document.Lock
	Runtime      *discovery.Selection
	RuntimeFiles []AdoptedRuntimeFile
	Packages     []AdoptedPackage
	Logger       *log.Logger
	// PublishLock publishes the resolved lock with the adoption. Callers that
	// reused an existing lock byte-for-byte leave it false.
	PublishLock bool
	// ManifestSnapshot encodes a fresh manifest. ManifestMerge merges into the
	// bytes already on disk; nil replaces them, matching the configured
	// document service (WithoutMerge/WithEncoder).
	ManifestSnapshot func() ([]byte, error)
	ManifestMerge    func([]byte) ([]byte, error)
	// Preconditions pins the document bytes the operation observed right after
	// recovery. Publication fails instead of overwriting a document that
	// changed while the operation resolved. The zero value captures the state
	// at adoption start.
	Preconditions AdoptionPreconditions
}

// AdoptionPreconditions pins the manifest, lock, and receipt bytes an
// operation started from. Capture it under the workspace writer lock
// immediately after journal recovery, before resolution reads the workspace.
type AdoptionPreconditions struct {
	captured  bool
	documents map[string]adoptionPinned
}

// adoptionPinned is one document state captured before planning. Exists false
// records an absent path, which is distinct from a present empty file.
type adoptionPinned struct {
	exists bool
	raw    []byte
	mode   os.FileMode
	image  *workspacewrite.InstallImage
}

// CaptureAdoptionPreconditions records the current state of the documents
// adoption may publish. The caller must hold the workspace writer lock and
// must run journal recovery first, so the captured state is the recovered one.
func CaptureAdoptionPreconditions(root string) (AdoptionPreconditions, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return AdoptionPreconditions{}, err
	}
	documents := map[string]adoptionPinned{
		document.ManifestFilename: {},
		document.LockFilename:     {},
		receipt.Filename:          {},
	}
	for name := range documents {
		pinned, err := captureAdoptionDocument(root, name)
		if err != nil {
			return AdoptionPreconditions{}, err
		}
		documents[name] = pinned
	}
	return AdoptionPreconditions{captured: true, documents: documents}, nil
}

func captureAdoptionDocument(root, name string) (adoptionPinned, error) {
	if err := workspacewrite.SafePath(root, name); err != nil {
		return adoptionPinned{}, err
	}
	path := filepath.Join(root, filepath.FromSlash(name))
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return adoptionPinned{}, nil
	case err != nil:
		return adoptionPinned{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return adoptionPinned{}, fmt.Errorf(
			"adoption document %s is not a regular file",
			name,
		)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return adoptionPinned{}, err
	}
	mode := info.Mode().Perm()
	return adoptionPinned{
		exists: true,
		raw:    raw,
		mode:   mode,
		image:  workspacewrite.InstallBytesImage(raw, mode),
	}, nil
}

// AdoptExistingWorkspace explicitly adopts an existing workspace installation
// without invoking the installer. It verifies attributable runtime files and
// identified packages, checks bootstrap contract satisfaction against the
// locked server, enforces path and symlink safety, and atomically publishes
// the document and receipt changes under the standard installation journal
// (.lucy/commit.yaml and rollback backup).
//
// If the locked bootstrap contract is fully satisfied by existing regular
// files, the receipt records the bootstrap fingerprint so a subsequent install
// skips bootstrap. If satisfaction is inconclusive, explicit ownership of
// recognized files is still recorded (preventing future collision conflicts)
// but the bootstrap fingerprint stays empty. Existing baselines are never
// reset: a managed file whose digest or mode changed fails adoption.
func AdoptExistingWorkspace(root string, opts AdoptOptions) error {
	if opts.Lock == nil {
		return errors.New("cannot adopt workspace without lock")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	if !opts.Preconditions.captured {
		opts.Preconditions, err = CaptureAdoptionPreconditions(root)
		if err != nil {
			return err
		}
	}
	tracked := trackedPaths(opts.Lock)

	current, legacy, ledger, err := loadAdoptionReceipt(root, tracked)
	if err != nil {
		return err
	}
	if err := adoptRuntime(root, opts, tracked, ledger); err != nil {
		return err
	}
	if err := adoptPackages(root, opts, tracked, ledger); err != nil {
		return err
	}
	if err := adoptMCDRWrapper(root, opts, tracked, ledger); err != nil {
		return err
	}
	if err := ledger.rejectUnclaimedLegacy(); err != nil {
		return err
	}
	next, err := ledger.build(root, opts.Lock)
	if err != nil {
		return err
	}

	expectations := make([]adoptionExpectation, 0, len(next.Entries))
	for _, entry := range next.Entries {
		expectations = append(expectations, adoptionExpectation{
			entry:   entry,
			present: ledger.verifiedPresent[entry.Path],
		})
	}
	publishReceipt := adoptionReceiptChanged(
		current,
		legacy,
		next,
		opts.Preconditions.documents[receipt.Filename].exists,
	)
	return publishAdoption(root, opts, next, publishReceipt, expectations)
}

// adoptionLedger accumulates ownership claims over the receipt read at
// adoption start. It keeps accepted baselines separate from the entries being
// built so a claim can never silently reset a protected file.
type adoptionLedger struct {
	previous             map[string]receipt.Entry
	entries              map[string]receipt.Entry
	verifiedPresent      map[string]bool
	legacyUnclaimed      map[string]struct{}
	bootstrapFingerprint string
}

func newAdoptionLedger() *adoptionLedger {
	return &adoptionLedger{
		previous:        make(map[string]receipt.Entry),
		entries:         make(map[string]receipt.Entry),
		verifiedPresent: make(map[string]bool),
		legacyUnclaimed: make(map[string]struct{}),
	}
}

// loadAdoptionReceipt reads the existing receipt and preserves its accepted
// baselines. Legacy entries lack origin attribution, so they are held aside
// until adoption re-identifies their paths; an entry that stays unattributed
// fails closed instead of entering a receipt that cannot validate.
func loadAdoptionReceipt(
	root string,
	tracked map[string]bool,
) (*receipt.Receipt, bool, *adoptionLedger, error) {
	current, err := receipt.Read(root)
	legacy := false
	var legacyErr *receipt.ErrLegacyReceipt
	switch {
	case errors.As(err, &legacyErr):
		current = legacyErr.Receipt
		legacy = true
	case errors.Is(err, os.ErrNotExist):
		current = nil
	case err != nil:
		return nil, false, nil, fmt.Errorf("read existing receipt: %w", err)
	}

	ledger := newAdoptionLedger()
	if current == nil {
		return nil, false, ledger, nil
	}
	if current.Version != receipt.Version {
		return nil, false, nil, fmt.Errorf(
			"unsupported installation receipt version %d",
			current.Version,
		)
	}
	for _, entry := range current.Entries {
		cleanPath := cleanWorkspacePath(entry.Path)
		if !workspacePathSafe(root, cleanPath) {
			return nil, false, nil, fmt.Errorf(
				"unsafe existing receipt path %q",
				entry.Path,
			)
		}
		if tracked[strings.ToLower(cleanPath)] {
			return nil, false, nil, fmt.Errorf(
				"tracked file %s collides with previously managed output in receipt",
				cleanPath,
			)
		}
		present, err := verifyExistingBaseline(root, cleanPath, entry)
		if err != nil {
			return nil, false, nil, err
		}
		ledger.previous[cleanPath] = entry
		ledger.verifiedPresent[cleanPath] = present
		if !legacy || attributedOrigin(entry.Origin) {
			ledger.entries[cleanPath] = entry
			continue
		}
		ledger.legacyUnclaimed[cleanPath] = struct{}{}
	}
	return current, legacy, ledger, nil
}

// claim records that adoption owns path with the given origin and identity.
// An existing entry or accepted baseline must agree; a changed digest, mode,
// origin, or package identity fails closed so adoption never resets a
// protected file.
func (l *adoptionLedger) claim(
	path string,
	origin string,
	packageID string,
	sha256 string,
	mode uint32,
) error {
	if prior, exists := l.entries[path]; exists {
		if prior.Origin != origin || prior.Package != packageID {
			return fmt.Errorf(
				"path %s has conflicting ownership: %s/%s vs %s/%s",
				path, prior.Origin, prior.Package, origin, packageID,
			)
		}
		return nil
	}

	prior, exists := l.previous[path]
	if !exists {
		l.entries[path] = receipt.Entry{
			Path:    path,
			SHA256:  sha256,
			Mode:    mode,
			Origin:  origin,
			Package: packageID,
		}
		l.verifiedPresent[path] = true
		return nil
	}

	if prior.SHA256 == "" {
		return fmt.Errorf(
			"managed output %s has no accepted baseline; remove its receipt entry before re-initializing",
			path,
		)
	}
	if sha256 != "" && !strings.EqualFold(prior.SHA256, sha256) {
		return fmt.Errorf(
			"locally modified managed output %s cannot be re-initialized without explicit conflict resolution",
			path,
		)
	}
	if prior.Mode != 0 && mode != 0 && prior.Mode != mode {
		return fmt.Errorf(
			"managed output %s changed mode from %#o to %#o; resolve the modification before re-initializing",
			path, prior.Mode, mode,
		)
	}
	if prior.Origin != "" && prior.Origin != origin &&
		!(prior.Origin == receipt.OriginBootstrap && origin == receipt.OriginMCDR) {
		return fmt.Errorf(
			"path %s has conflicting ownership origins: %s and %s",
			path, prior.Origin, origin,
		)
	}
	if prior.Origin == receipt.OriginPackage &&
		prior.Package != "" && packageID != "" && prior.Package != packageID {
		return fmt.Errorf(
			"path %s has conflicting package identity: %s and %s",
			path, prior.Package, packageID,
		)
	}

	// Preserve the accepted baseline the local edit is measured against; a
	// legacy entry without a recorded mode adopts the observed one.
	baselineMode := prior.Mode
	if baselineMode == 0 {
		baselineMode = mode
	}
	l.entries[path] = receipt.Entry{
		Path:    path,
		SHA256:  prior.SHA256,
		Mode:    baselineMode,
		Origin:  origin,
		Package: packageID,
	}
	l.verifiedPresent[path] = true
	delete(l.legacyUnclaimed, path)
	return nil
}

// rejectUnclaimedLegacy fails when a legacy entry could not be re-identified
// by this explicit adoption, because its origin and protection cannot be
// carried into a modern receipt safely.
func (l *adoptionLedger) rejectUnclaimedLegacy() error {
	if len(l.legacyUnclaimed) == 0 {
		return nil
	}
	unclaimed := make([]string, 0, len(l.legacyUnclaimed))
	for path := range l.legacyUnclaimed {
		unclaimed = append(unclaimed, path)
	}
	slices.Sort(unclaimed)
	return fmt.Errorf(
		"legacy installation receipt records paths initialization could not re-identify: %s; "+
			"restore the files and rerun lucy init --force, or delete %s and rerun "+
			"lucy init --force to rebuild the receipt from observed files",
		strings.Join(unclaimed, ", "),
		receipt.Filename,
	)
}

// build finalizes the receipt: package fingerprint, sorted entries, alias
// rejection, and schema validation.
func (l *adoptionLedger) build(root string, lock *document.Lock) (receipt.Receipt, error) {
	var packageFingerprint string
	if packageSetObserved(root, lock, l.entries) {
		fingerprint, err := receipt.PackageFingerprint(lock)
		if err != nil {
			return receipt.Receipt{}, err
		}
		packageFingerprint = fingerprint
	}

	names := make([]string, 0, len(l.entries))
	for path := range l.entries {
		names = append(names, path)
	}
	slices.Sort(names)
	if err := rejectInstallPathAliases(root, names); err != nil {
		return receipt.Receipt{}, fmt.Errorf(
			"conflicting path aliases in adopted files: %w",
			err,
		)
	}

	next := receipt.New()
	next.BootstrapFingerprint = l.bootstrapFingerprint
	next.PackageFingerprint = packageFingerprint
	next.Entries = make([]receipt.Entry, 0, len(names))
	for _, name := range names {
		next.Entries = append(next.Entries, l.entries[name])
	}
	if err := next.Validate(); err != nil {
		return receipt.Receipt{}, fmt.Errorf("validate adopted receipt: %w", err)
	}
	return next, nil
}

func attributedOrigin(origin string) bool {
	switch origin {
	case receipt.OriginBootstrap, receipt.OriginPackage, receipt.OriginMCDR:
		return true
	default:
		return false
	}
}
