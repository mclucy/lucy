package install

import (
	"bytes"
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
	Runtime      *discovery.Installation
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
	Preconditions WorkspaceDocuments
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
func AdoptExistingWorkspace(root string, opts AdoptOptions) (resultErr error) {
	if opts.Lock == nil {
		return errors.New("cannot adopt workspace without lock")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	if opts.Preconditions.documents == nil {
		opts.Preconditions, err = CaptureWorkspaceDocuments(root)
		if err != nil {
			return err
		}
	}
	files, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, files.Close()) }()
	tracked := trackedPaths(opts.Lock)

	current, legacy, ledger, err := loadAdoptionReceipt(
		files,
		root,
		opts.Preconditions,
		tracked,
	)
	if err != nil {
		return err
	}
	if err := adoptRuntime(files, root, opts, tracked, ledger); err != nil {
		return err
	}
	if err := adoptPackages(files, root, opts, tracked, ledger); err != nil {
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

	publishReceipt := adoptionReceiptChanged(
		current,
		legacy,
		next,
		opts.Preconditions.documents[receipt.Filename].image != nil,
	)
	return publishAdoption(root, opts, next, publishReceipt, ledger.images)
}

// adoptionLedger accumulates ownership claims over the receipt read at
// adoption start. It keeps accepted baselines separate from the entries being
// built so a claim can never silently reset a protected file.
type adoptionLedger struct {
	previous             map[string]receipt.Entry
	entries              map[string]receipt.Entry
	images               map[string]*workspacewrite.InstallImage
	legacyUnclaimed      map[string]struct{}
	bootstrapFingerprint string
}

func newAdoptionLedger() *adoptionLedger {
	return &adoptionLedger{
		previous:        make(map[string]receipt.Entry),
		entries:         make(map[string]receipt.Entry),
		images:          make(map[string]*workspacewrite.InstallImage),
		legacyUnclaimed: make(map[string]struct{}),
	}
}

// loadAdoptionReceipt decodes the receipt captured at adoption start and
// preserves its accepted baselines, without reading the document a second
// time. Legacy entries lack origin attribution, so they are held aside until
// adoption re-identifies their paths; an entry that stays unattributed fails
// closed instead of entering a receipt that cannot validate.
func loadAdoptionReceipt(
	files *os.Root,
	root string,
	documents WorkspaceDocuments,
	tracked map[string]bool,
) (*receipt.Receipt, bool, *adoptionLedger, error) {
	captured, ok := documents.documents[receipt.Filename]
	if !ok {
		return nil, false, nil, fmt.Errorf(
			"adoption preconditions do not pin %s",
			receipt.Filename,
		)
	}
	var current *receipt.Receipt
	legacy := false
	if captured.image != nil {
		parsed, err := receipt.Decode(bytes.NewReader(captured.raw))
		var legacyErr *receipt.ErrLegacyReceipt
		switch {
		case errors.As(err, &legacyErr):
			current = legacyErr.Receipt
			legacy = true
		case err != nil:
			return nil, false, nil, fmt.Errorf("read existing receipt: %w", err)
		default:
			current = parsed
		}
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
		image, err := verifyExistingBaseline(files, cleanPath, entry)
		if err != nil {
			return nil, false, nil, err
		}
		ledger.previous[cleanPath] = entry
		ledger.images[cleanPath] = image
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
	observed *workspacewrite.InstallImage,
) error {
	if observed.Mode == 0 {
		return fmt.Errorf(
			"cannot adopt %s with mode 0000: the receipt cannot record it as an observed mode",
			path,
		)
	}

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
			SHA256:  observed.SHA256,
			Mode:    observed.Mode,
			Origin:  origin,
			Package: packageID,
		}
		l.images[path] = observed
		return nil
	}

	if prior.SHA256 == "" {
		return fmt.Errorf(
			"managed output %s has no accepted baseline; remove its receipt entry before re-initializing",
			path,
		)
	}
	if observed.SHA256 != "" && !strings.EqualFold(prior.SHA256, observed.SHA256) {
		return fmt.Errorf(
			"locally modified managed output %s cannot be re-initialized without explicit conflict resolution",
			path,
		)
	}
	if prior.Mode != 0 && workspacewrite.EffectiveInstallMode(
		os.FileMode(prior.Mode),
	) != observed.Mode {
		return fmt.Errorf(
			"managed output %s changed mode from %#o to %#o; resolve the modification before re-initializing",
			path, prior.Mode, observed.Mode,
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
	// legacy entry without a recorded mode adopts the observed one. Keep the
	// image observed when the receipt was read, so a missing path stays a
	// missing-path precondition and any reappearance is a conflict.
	baselineMode := prior.Mode
	if baselineMode == 0 {
		baselineMode = observed.Mode
	}
	l.entries[path] = receipt.Entry{
		Path:    path,
		SHA256:  prior.SHA256,
		Mode:    baselineMode,
		Origin:  origin,
		Package: packageID,
	}
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
	if err := rejectCommitPathAliases(root, "adoption", names); err != nil {
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
