package install

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mclucy/lucy/bootstrap"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/fsutil"
	"github.com/mclucy/lucy/internal/workspaceguard"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/receipt"
	"github.com/mclucy/lucy/workspace/discovery"
	"gopkg.in/yaml.v3"
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
		image:  installBytesImage(raw, mode),
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

	// 1. Read the receipt and preserve accepted baselines. Legacy entries lack
	// origin attribution, so they are held aside until adoption re-identifies
	// their paths; an entry that stays unattributed fails closed with explicit
	// remediation instead of entering a receipt that cannot validate.
	current, err := receipt.Read(root)
	legacyReceipt := false
	var legacyErr *receipt.ErrLegacyReceipt
	if errors.As(err, &legacyErr) {
		current = legacyErr.Receipt
		legacyReceipt = true
		err = nil
	} else if errors.Is(err, os.ErrNotExist) {
		err = nil
	} else if err != nil {
		return fmt.Errorf("read existing receipt: %w", err)
	}
	previous := make(map[string]receipt.Entry)
	verifiedPresent := make(map[string]bool)
	entries := make(map[string]receipt.Entry)
	legacyUnclaimed := make(map[string]struct{})
	if current != nil && err == nil {
		if current.Version != receipt.Version {
			return fmt.Errorf("unsupported installation receipt version %d", current.Version)
		}
		for _, e := range current.Entries {
			cleanPath := filepath.ToSlash(filepath.Clean(filepath.FromSlash(e.Path)))
			if !filepath.IsLocal(filepath.FromSlash(cleanPath)) ||
				workspacewrite.SafePath(root, cleanPath) != nil {
				return fmt.Errorf("unsafe existing receipt path %q", e.Path)
			}
			if tracked[strings.ToLower(cleanPath)] {
				return fmt.Errorf(
					"tracked file %s collides with previously managed output in receipt",
					cleanPath,
				)
			}
			present, err := verifyExistingBaseline(root, cleanPath, e)
			if err != nil {
				return err
			}
			previous[cleanPath] = e
			verifiedPresent[cleanPath] = present
			switch {
			case !legacyReceipt:
				entries[cleanPath] = e
			case e.Origin == receipt.OriginBootstrap ||
				e.Origin == receipt.OriginPackage ||
				e.Origin == receipt.OriginMCDR:
				// Origin already attributes this entry; only the legacy lock
				// fingerprint field needs clearing.
				entries[cleanPath] = e
			default:
				legacyUnclaimed[cleanPath] = struct{}{}
			}
		}
	}

	claim := func(
		cleanRel string,
		origin string,
		pkgID string,
		sha256 string,
		mode uint32,
	) error {
		if prior, exists := entries[cleanRel]; exists {
			if prior.Origin != origin || prior.Package != pkgID {
				return fmt.Errorf(
					"path %s has conflicting ownership: %s/%s vs %s/%s",
					cleanRel, prior.Origin, prior.Package, origin, pkgID,
				)
			}
			return nil
		}
		if prior, exists := previous[cleanRel]; exists {
			if prior.SHA256 == "" {
				return fmt.Errorf(
					"managed output %s has no accepted baseline; remove its receipt entry before re-initializing",
					cleanRel,
				)
			}
			if sha256 != "" && !strings.EqualFold(prior.SHA256, sha256) {
				return fmt.Errorf(
					"locally modified managed output %s cannot be re-initialized without explicit conflict resolution",
					cleanRel,
				)
			}
			if prior.Mode != 0 && mode != 0 && prior.Mode != mode {
				return fmt.Errorf(
					"managed output %s changed mode from %#o to %#o; resolve the modification before re-initializing",
					cleanRel, prior.Mode, mode,
				)
			}
			if prior.Origin != "" && prior.Origin != origin &&
				!(prior.Origin == receipt.OriginBootstrap &&
					origin == receipt.OriginMCDR) {
				return fmt.Errorf(
					"path %s has conflicting ownership origins: %s and %s",
					cleanRel, prior.Origin, origin,
				)
			}
			if prior.Origin == receipt.OriginPackage &&
				prior.Package != "" && pkgID != "" && prior.Package != pkgID {
				return fmt.Errorf(
					"path %s has conflicting package identity: %s and %s",
					cleanRel, prior.Package, pkgID,
				)
			}
			// Preserve the accepted baseline the local edit is measured
			// against; a legacy entry without a recorded mode adopts the
			// observed one.
			baselineMode := prior.Mode
			if baselineMode == 0 {
				baselineMode = mode
			}
			entries[cleanRel] = receipt.Entry{
				Path:    cleanRel,
				SHA256:  prior.SHA256,
				Mode:    baselineMode,
				Origin:  origin,
				Package: pkgID,
			}
			verifiedPresent[cleanRel] = true
			delete(legacyUnclaimed, cleanRel)
			return nil
		}
		entries[cleanRel] = receipt.Entry{
			Path:    cleanRel,
			SHA256:  sha256,
			Mode:    mode,
			Origin:  origin,
			Package: pkgID,
		}
		verifiedPresent[cleanRel] = true
		return nil
	}

	next := receipt.New()

	// 2. Runtime matching and bootstrap contract verification.
	if opts.Lock.Server.Bootstrapper != "" || opts.Lock.Server.Product != "" {
		serverDir := filepath.Join(root, filepath.FromSlash(opts.Lock.Server.Directory))
		checkResult, checkErr := bootstrap.CheckExisting(serverDir, opts.Lock.Server)
		if checkErr != nil && opts.Runtime != nil {
			return fmt.Errorf("verify existing server runtime: %w", checkErr)
		}
		runtimeFileMap := make(map[string]AdoptedRuntimeFile, len(opts.RuntimeFiles))
		for _, rf := range opts.RuntimeFiles {
			runtimeFileMap[filepath.Clean(rf.Path)] = rf
		}
		entryPointRel := ""
		if opts.Runtime != nil && opts.Runtime.EntryPoint != nil {
			rel, relErr := filepath.Rel(root, opts.Runtime.EntryPoint.Path)
			if relErr == nil && filepath.IsLocal(rel) {
				entryPointRel = filepath.ToSlash(filepath.Clean(rel))
			}
		}

		if checkResult.Current && entryPointRel != "" {
			// The verified tree must contain the runtime observation selected.
			// Another shared library inside the same installation files must
			// not prove that the selected entry point satisfies the contract.
			selectedCore := false
			for _, out := range checkResult.Outputs {
				if out.Role != bootstrap.RoleRuntime {
					continue
				}
				cleanRel := filepath.ToSlash(filepath.Clean(filepath.FromSlash(out.Path)))
				if cleanRel == entryPointRel {
					selectedCore = true
					break
				}
			}
			if !selectedCore {
				checkResult.Current = false
				checkResult.Status = bootstrap.CurrentnessMismatch
				checkResult.Reason = "selected runtime does not match verified server output"
			}
		}

		if checkResult.Current {
			allOutputsValid := true
			for _, out := range checkResult.Outputs {
				if out.Policy != bootstrap.PolicyManaged || out.Role != bootstrap.RoleRuntime {
					continue
				}
				cleanRel := filepath.ToSlash(filepath.Clean(filepath.FromSlash(out.Path)))
				if !filepath.IsLocal(filepath.FromSlash(cleanRel)) ||
					workspacewrite.SafePath(root, cleanRel) != nil {
					return fmt.Errorf("verified bootstrap output %s escapes the workspace", out.Path)
				}
				if tracked[strings.ToLower(cleanRel)] || installPathReserved(root, cleanRel) {
					allOutputsValid = false
					break
				}
				target := filepath.Join(root, filepath.FromSlash(cleanRel))
				st, statErr := os.Lstat(target)
				if statErr != nil || st.Mode()&os.ModeSymlink != 0 || !st.Mode().IsRegular() {
					allOutputsValid = false
					break
				}
				actual, digestErr := DigestFile(target, nil)
				if digestErr != nil || !strings.EqualFold(actual["sha256"], out.SHA256) ||
					uint32(st.Mode().Perm()) != out.Mode {
					allOutputsValid = false
					break
				}
				if rf, observed := runtimeFileMap[filepath.Clean(target)]; observed {
					if !strings.EqualFold(rf.ExpectedSHA256, out.SHA256) ||
						(rf.ExpectedMode != 0 && rf.ExpectedMode != out.Mode) {
						allOutputsValid = false
						break
					}
				}
			}

			if !allOutputsValid {
				return errors.New("verified server outputs changed or conflict with tracked files during adoption")
			}
			if allOutputsValid {
				if entryPointRel != "" {
					if _, observed := runtimeFileMap[filepath.Clean(
						filepath.Join(root, filepath.FromSlash(entryPointRel)),
					)]; !observed {
						return fmt.Errorf(
							"selected entry point %s has no observed baseline",
							entryPointRel,
						)
					}
				}
				bfp, bfpErr := receipt.BootstrapFingerprint(opts.Lock)
				if bfpErr != nil {
					return bfpErr
				}
				next.BootstrapFingerprint = bfp
				if opts.Logger != nil {
					opts.Logger.Debug(
						"existing server tree satisfies locked bootstrap contract",
						"method", opts.Lock.Server.Bootstrapper,
						"fingerprint", bfp,
						"outputs", len(checkResult.Outputs),
					)
				}
				for _, out := range checkResult.Outputs {
					if out.Policy != bootstrap.PolicyManaged || out.Role != bootstrap.RoleRuntime {
						continue
					}
					cleanRel := filepath.ToSlash(filepath.Clean(filepath.FromSlash(out.Path)))
					if err := claim(
						cleanRel,
						receipt.OriginBootstrap,
						"",
						out.SHA256,
						out.Mode,
					); err != nil {
						return err
					}
				}
			}
		} else {
			if opts.Logger != nil {
				opts.Logger.Debug(
					"recording attributable runtime ownership without bootstrap currentness",
					"reason", checkResult.Reason,
				)
			}
			for _, out := range checkResult.Outputs {
				if out.Policy != bootstrap.PolicyManaged || out.Role != bootstrap.RoleRuntime {
					continue
				}
				cleanRel := filepath.ToSlash(filepath.Clean(filepath.FromSlash(out.Path)))
				if !filepath.IsLocal(filepath.FromSlash(cleanRel)) ||
					workspacewrite.SafePath(root, cleanRel) != nil ||
					tracked[strings.ToLower(cleanRel)] || installPathReserved(root, cleanRel) {
					return fmt.Errorf("verified bootstrap output %s conflicts with workspace state", out.Path)
				}
				target := filepath.Join(root, filepath.FromSlash(cleanRel))
				info, err := os.Lstat(target)
				if err != nil || !info.Mode().IsRegular() {
					return fmt.Errorf("verified bootstrap output %s is no longer a regular file", out.Path)
				}
				hashes, err := DigestFile(target, nil)
				if err != nil {
					return err
				}
				if !strings.EqualFold(hashes["sha256"], out.SHA256) || uint32(info.Mode().Perm()) != out.Mode {
					return fmt.Errorf("verified bootstrap output %s changed during adoption", out.Path)
				}
				if err := claim(cleanRel, receipt.OriginBootstrap, "", out.SHA256, out.Mode); err != nil {
					return err
				}
			}
			specs := bootstrap.OutputSpecs(opts.Lock.Server)
			serverDirRel := filepath.ToSlash(filepath.Clean(filepath.FromSlash(opts.Lock.Server.Directory)))
			if serverDirRel == "." {
				serverDirRel = ""
			}

			if opts.Runtime != nil && opts.Runtime.Installation != nil {
				for _, f := range opts.Runtime.Installation.Files {
					rel, relErr := filepath.Rel(root, f.Path)
					if relErr != nil || !filepath.IsLocal(rel) {
						return fmt.Errorf("runtime file %s escapes the workspace", f.Path)
					}
					cleanRel := filepath.ToSlash(filepath.Clean(rel))
					if workspacewrite.SafePath(root, cleanRel) != nil ||
						tracked[strings.ToLower(cleanRel)] ||
						installPathReserved(root, cleanRel) {
						continue
					}
					serverRel := cleanRel
					if serverDirRel != "" {
						if !strings.HasPrefix(cleanRel, serverDirRel+"/") &&
							cleanRel != serverDirRel {
							continue
						}
						serverRel = strings.TrimPrefix(
							strings.TrimPrefix(cleanRel, serverDirRel), "/",
						)
					}
					if !managedRuntimeOutput(specs, serverRel) {
						continue
					}
					rf, observed := runtimeFileMap[filepath.Clean(f.Path)]
					if !observed && filepath.IsLocal(cleanRel) {
						info, statErr := os.Lstat(f.Path)
						if statErr == nil && info.Mode()&os.ModeSymlink != 0 {
							// Aliases stay unowned.
							continue
						}
					}
					if !observed || rf.ExpectedSHA256 == "" || rf.ExpectedMode == 0 {
						return fmt.Errorf(
							"runtime file %s has no observed baseline",
							cleanRel,
						)
					}
					st, statErr := os.Lstat(f.Path)
					if statErr != nil || !st.Mode().IsRegular() {
						return fmt.Errorf(
							"runtime file %s is no longer a regular file",
							cleanRel,
						)
					}
					hashes, digestErr := DigestFile(f.Path, nil)
					if digestErr != nil {
						return digestErr
					}
					if !strings.EqualFold(hashes["sha256"], rf.ExpectedSHA256) ||
						uint32(st.Mode().Perm()) != rf.ExpectedMode {
						return fmt.Errorf(
							"runtime file %s changed between discovery and adoption publication",
							cleanRel,
						)
					}
					if err := claim(
						cleanRel,
						receipt.OriginBootstrap,
						"",
						hashes["sha256"],
						uint32(st.Mode().Perm()),
					); err != nil {
						return err
					}
				}
			}
		}
	}

	// 3. Package adoption.
	for _, pkg := range opts.Packages {
		if pkg.Path == "" {
			continue
		}
		fullPath := pkg.Path
		if !filepath.IsAbs(fullPath) {
			fullPath = filepath.Join(root, fullPath)
		}
		rel, relErr := filepath.Rel(root, fullPath)
		if relErr != nil || !filepath.IsLocal(rel) {
			return fmt.Errorf("package file %s escapes the workspace", pkg.Path)
		}
		cleanRel := filepath.ToSlash(filepath.Clean(rel))
		if err := workspacewrite.SafePath(root, cleanRel); err != nil {
			return err
		}
		if tracked[strings.ToLower(cleanRel)] || installPathReserved(root, cleanRel) {
			continue
		}
		st, statErr := os.Lstat(fullPath)
		if statErr != nil || st.Mode()&os.ModeSymlink != 0 || !st.Mode().IsRegular() {
			if opts.Logger != nil {
				opts.Logger.Debug("refuse non-regular or symlink package file", "path", cleanRel)
			}
			continue
		}
		hashes, digestErr := DigestFile(fullPath, nil)
		if digestErr != nil {
			return digestErr
		}
		if pkg.ExpectedSHA256 != "" &&
			!strings.EqualFold(hashes["sha256"], pkg.ExpectedSHA256) {
			return fmt.Errorf(
				"package file %s changed between discovery and adoption publication",
				cleanRel,
			)
		}
		if pkg.ExpectedMode != 0 && uint32(st.Mode().Perm()) != pkg.ExpectedMode {
			return fmt.Errorf(
				"package file %s changed mode between discovery and adoption publication",
				cleanRel,
			)
		}

		pkgID := pkg.PackageID
		if pkgID == "" && opts.Lock != nil {
			for i := range opts.Lock.Packages {
				lp := &opts.Lock.Packages[i]
				dest, destErr := packageDestination(opts.Lock, lp)
				if destErr == nil &&
					filepath.Clean(filepath.FromSlash(dest)) ==
						filepath.Clean(filepath.FromSlash(cleanRel)) {
					if lp.Artifact.ProjectID != "" {
						pkgID = lp.Artifact.Provider.String() + ":" + lp.Artifact.ProjectID
					} else {
						pkgID = lp.Filename
					}
					break
				}
			}
		}
		if err := claim(
			cleanRel,
			receipt.OriginPackage,
			pkgID,
			hashes["sha256"],
			uint32(st.Mode().Perm()),
		); err != nil {
			return err
		}
	}

	// 3b. The MCDR wrapper launcher is managed output when installation
	// generates it. Initialization selects no isolated environment, so only an
	// existing wrapper whose bytes exactly match the generated global launcher
	// is recorded; a customized or venv-specific wrapper stays unowned.
	if opts.Lock.MCDR != nil {
		relative := filepath.ToSlash(filepath.Join(
			filepath.FromSlash(opts.Lock.MCDR.Directory),
			"run-mcdr.sh",
		))
		if filepath.IsLocal(filepath.FromSlash(relative)) &&
			workspacewrite.SafePath(root, relative) == nil &&
			!tracked[strings.ToLower(relative)] &&
			!installPathReserved(root, relative) {
			target := filepath.Join(root, filepath.FromSlash(relative))
			info, statErr := os.Lstat(target)
			switch {
			case statErr == nil && info.Mode().IsRegular():
				data, readErr := os.ReadFile(target)
				if readErr != nil {
					return readErr
				}
				matched := false
				for _, variant := range mcdrLauncherVariants(opts.Lock) {
					if !bytes.Equal(data, variant.Script) {
						continue
					}
					if variant.Interpreter != "" {
						// An isolated launcher is only valid while the
						// interpreter it names exists in the workspace.
						if workspacewrite.SafePath(root, variant.Interpreter) != nil {
							continue
						}
						interpreter, statErr := os.Lstat(
							filepath.Join(root, filepath.FromSlash(variant.Interpreter)),
						)
						if statErr != nil || !interpreter.Mode().IsRegular() {
							continue
						}
					}
					hashes, digestErr := DigestFile(target, nil)
					if digestErr != nil {
						return digestErr
					}
					if err := claim(
						relative,
						receipt.OriginMCDR,
						"",
						hashes["sha256"],
						uint32(info.Mode().Perm()),
					); err != nil {
						return err
					}
					matched = true
					break
				}
				if !matched && opts.Logger != nil {
					opts.Logger.Debug(
						"MCDR wrapper launcher left unowned; bytes do not match a generated launcher",
						"path", relative,
					)
				}
			case statErr != nil && !errors.Is(statErr, os.ErrNotExist):
				return statErr
			}
		}
	}

	// 4. Every legacy entry must be re-identified by this explicit adoption;
	// otherwise its origin and protection cannot be carried into a modern
	// receipt safely.
	if len(legacyUnclaimed) > 0 {
		unclaimed := make([]string, 0, len(legacyUnclaimed))
		for path := range legacyUnclaimed {
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

	// 5. Fingerprints describe the resolved lock only when the observed tree
	// proves it. A workspace with no packages needs no package fingerprint.
	if len(opts.Lock.Packages) > 0 {
		allMatch := true
		for i := range opts.Lock.Packages {
			lp := &opts.Lock.Packages[i]
			if !lp.Enabled() {
				continue
			}
			dest, destErr := packageDestination(opts.Lock, lp)
			if destErr != nil {
				allMatch = false
				break
			}
			cleanDest := filepath.ToSlash(filepath.Clean(dest))
			entry, ok := entries[cleanDest]
			if !ok || entry.Origin != receipt.OriginPackage {
				allMatch = false
				break
			}
			expectedID := lp.Artifact.Provider.String() + ":" + lp.Artifact.ProjectID
			if lp.Artifact.ProjectID != "" && entry.Package != "" &&
				entry.Package != expectedID && entry.Package != lp.Filename {
				allMatch = false
				break
			}
			target := filepath.Join(root, filepath.FromSlash(cleanDest))
			if len(lp.Artifact.Hashes) > 0 {
				if _, digestErr := DigestFile(target, lp.Artifact.Hashes); digestErr != nil {
					allMatch = false
					break
				}
			} else {
				allMatch = false
				break
			}
		}
		if allMatch {
			pfp, pErr := receipt.PackageFingerprint(opts.Lock)
			if pErr == nil {
				next.PackageFingerprint = pfp
			}
		}
	}

	// 6. Sorting, validation, and collision aliases check.
	names := make([]string, 0, len(entries))
	for path := range entries {
		names = append(names, path)
	}
	slices.Sort(names)
	if err := rejectInstallPathAliases(root, names); err != nil {
		return fmt.Errorf("conflicting path aliases in adopted files: %w", err)
	}
	next.Entries = make([]receipt.Entry, 0, len(entries))
	for _, name := range names {
		next.Entries = append(next.Entries, entries[name])
	}
	if err := next.Validate(); err != nil {
		return fmt.Errorf("validate adopted receipt: %w", err)
	}

	// 7. Atomic publication through the adoption transaction.
	publishReceipt := adoptionReceiptChanged(
		current,
		legacyReceipt,
		&next,
		opts.Preconditions.documents[receipt.Filename].exists,
	)
	expectations := make([]adoptionExpectation, 0, len(next.Entries))
	for _, entry := range next.Entries {
		expectations = append(expectations, adoptionExpectation{
			entry:   entry,
			present: verifiedPresent[entry.Path],
		})
	}
	return publishAdoption(root, opts, &next, publishReceipt, expectations)
}

// adoptionReceiptChanged reports whether adoption must rewrite the receipt.
// An unchanged receipt keeps its exact bytes, including hand-formatted fields;
// a legacy receipt always needs the modern origins Snapshot emits.
func adoptionReceiptChanged(
	previous *receipt.Receipt,
	legacy bool,
	next *receipt.Receipt,
	exists bool,
) bool {
	if !exists || previous == nil {
		return len(next.Entries) > 0 ||
			next.BootstrapFingerprint != "" ||
			next.PackageFingerprint != ""
	}
	if legacy || previous.LockFingerprint != "" ||
		previous.Version != next.Version ||
		previous.BootstrapFingerprint != next.BootstrapFingerprint ||
		previous.PackageFingerprint != next.PackageFingerprint {
		return true
	}
	prior := make(map[string]receipt.Entry, len(previous.Entries))
	for _, entry := range previous.Entries {
		prior[entry.Path] = entry
	}
	if len(prior) != len(next.Entries) {
		return true
	}
	for _, entry := range next.Entries {
		if prior[entry.Path] != entry {
			return true
		}
	}
	return false
}

// verifyExistingBaseline checks that a receipt entry still describes the file
// it protects and reports whether that file was present. Content and
// permission changes fail closed; a missing file keeps its ownership record
// for later repair.
func verifyExistingBaseline(
	root, cleanPath string,
	entry receipt.Entry,
) (bool, error) {
	target := filepath.Join(root, filepath.FromSlash(cleanPath))
	info, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect existing managed path %s: %w", cleanPath, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return false, fmt.Errorf("existing managed path %s is not a regular file", cleanPath)
	}
	if entry.SHA256 == "" {
		return false, fmt.Errorf(
			"existing managed path %s has no accepted baseline; remove its receipt entry before re-initializing",
			cleanPath,
		)
	}
	actual, err := DigestFile(target, nil)
	if err != nil {
		return false, fmt.Errorf("digest existing managed path %s: %w", cleanPath, err)
	}
	if !strings.EqualFold(actual["sha256"], entry.SHA256) {
		return false, fmt.Errorf(
			"locally modified managed output %s cannot be re-initialized without explicit conflict resolution",
			cleanPath,
		)
	}
	if entry.Mode != 0 && uint32(info.Mode().Perm()) != entry.Mode {
		return false, fmt.Errorf(
			"managed output %s changed mode from %#o to %#o; resolve the modification before re-initializing",
			cleanPath, entry.Mode, info.Mode().Perm(),
		)
	}
	return true, nil
}

// managedRuntimeOutput reports whether serverRel falls inside a managed
// runtime output declared by the locked method. Scope matching stops at a path
// component boundary, so "libraries/net" never authorizes
// "libraries/networking.jar".
func managedRuntimeOutput(specs []bootstrap.OutputSpec, serverRel string) bool {
	serverRel = filepath.ToSlash(filepath.Clean(filepath.FromSlash(serverRel)))
	for _, spec := range specs {
		if spec.Policy != bootstrap.PolicyManaged || spec.Role != bootstrap.RoleRuntime {
			continue
		}
		if spec.Path != "" {
			if serverRel == filepath.ToSlash(
				filepath.Clean(filepath.FromSlash(spec.Path)),
			) {
				return true
			}
			continue
		}
		scope := filepath.ToSlash(filepath.Clean(filepath.FromSlash(spec.Scope)))
		if scope == "." || scope == "" {
			continue
		}
		if serverRel == scope || strings.HasPrefix(serverRel, scope+"/") {
			return true
		}
	}
	return false
}

// adoptionExpectation is one receipt entry and whether its file was present
// when adoption observed it. Absent entries keep ownership; present entries
// must still match at the publication recheck.
type adoptionExpectation struct {
	entry   receipt.Entry
	present bool
}

// adoptionProtected is a pinned document the transaction either rewrites or
// treats as a read-only commit precondition. Both kinds must still match at
// the commit point, so an external edit cannot leave published documents
// describing a different lock.
type adoptionProtected struct {
	name  string
	image *workspacewrite.InstallImage
}

// publishAdoption journals the exact manifest, lock, and receipt bytes,
// applies them, rechecks every recorded file and reused document, and only
// then publishes the commit point. A failure before the commit point restores
// the pinned before images; recovery never overwrites an edit made after the
// interruption.
func publishAdoption(
	root string,
	opts AdoptOptions,
	next *receipt.Receipt,
	publishReceipt bool,
	expectations []adoptionExpectation,
) (resultErr error) {
	pre := opts.Preconditions
	files, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, files.Close()) }()
	// Every pinned document must still match the recovered state, including
	// documents this transaction reuses without rewriting.
	for _, name := range []string{
		document.ManifestFilename,
		document.LockFilename,
		receipt.Filename,
	} {
		pinned, ok := pre.documents[name]
		if !ok {
			return fmt.Errorf("adoption preconditions do not pin %s", name)
		}
		if err := verifyPinnedCurrent(files, name, pinned); err != nil {
			return err
		}
	}

	var documents []adoptionDocument
	if opts.Manifest != nil {
		pinned := pre.documents[document.ManifestFilename]
		snapshot := opts.ManifestSnapshot
		if snapshot == nil {
			snapshot = func() ([]byte, error) {
				return document.ManifestFile(root).Snapshot(opts.Manifest)
			}
		}
		data := pinned.raw
		if pinned.exists {
			if opts.ManifestMerge != nil {
				data, err = opts.ManifestMerge(pinned.raw)
				if err != nil {
					return err
				}
			} else {
				data, err = snapshot()
				if err != nil {
					return err
				}
			}
		} else {
			data, err = snapshot()
			if err != nil {
				return err
			}
		}
		documents = append(documents, adoptionDocument{
			name: document.ManifestFilename,
			plan: pinnedPlan(document.ManifestFilename, pinned, 0o644, data),
		})
	}
	if opts.Lock != nil && opts.PublishLock {
		pinned := pre.documents[document.LockFilename]
		data := pinned.raw
		if pinned.exists {
			data, err = opts.Lock.MergeBytes(pinned.raw)
		} else {
			data, err = opts.Lock.Snapshot()
		}
		if err != nil {
			return err
		}
		documents = append(documents, adoptionDocument{
			name: document.LockFilename,
			plan: pinnedPlan(document.LockFilename, pinned, 0o644, data),
		})
	}
	if next == nil {
		empty := receipt.New()
		next = &empty
	}
	if publishReceipt {
		pinned := pre.documents[receipt.Filename]
		data, err := next.Snapshot()
		if err != nil {
			return err
		}
		documents = append(documents, adoptionDocument{
			name: receipt.Filename,
			plan: pinnedPlan(receipt.Filename, pinned, 0o600, data),
		})
	}
	protected := make([]adoptionProtected, 0, len(pre.documents))
	for _, name := range []string{
		document.ManifestFilename,
		document.LockFilename,
		receipt.Filename,
	} {
		protected = append(protected, adoptionProtected{
			name:  name,
			image: pre.documents[name].image,
		})
	}
	return publishAdoptionDocuments(
		root,
		documents,
		expectations,
		protected,
		opts.Logger,
	)
}

// verifyPinnedCurrent rejects a document that changed after the operation
// captured its preconditions, so publication never overwrites external edits.
func verifyPinnedCurrent(
	files *os.Root,
	name string,
	pinned adoptionPinned,
) error {
	before, err := observeInstallPath(files, name)
	if err != nil {
		return err
	}
	if !workspacewrite.InstallImagesEqual(before, pinned.image) {
		return fmt.Errorf(
			"adoption document %s changed after recovery; refusing to overwrite it",
			name,
		)
	}
	return nil
}

// pinnedPlan binds exact bytes to the before image captured for the document
// and the mode the publication preserves.
func pinnedPlan(
	name string,
	pinned adoptionPinned,
	defaultMode os.FileMode,
	data []byte,
) *installDocumentPlan {
	mode := defaultMode
	var base *workspacewrite.InstallImage
	if pinned.exists {
		// The captured mode already reflects the platform's permission
		// representation; reusing it preserves the file's permissions.
		mode = os.FileMode(pinned.image.Mode)
		base = pinned.image
	}
	return &installDocumentPlan{
		Data:  data,
		Base:  base,
		Image: *installBytesImage(data, mode),
	}
}

// adoptionDocument is one metadata file the adoption transaction publishes.
// The plan binds the bytes to the before image the operation captured, so
// publication cannot merge into a document that changed after recovery.
type adoptionDocument struct {
	name string
	plan *installDocumentPlan
}

func publishAdoptionDocuments(
	root string,
	documents []adoptionDocument,
	expected []adoptionExpectation,
	protected []adoptionProtected,
	logger *log.Logger,
) (resultErr error) {
	names := make([]string, 0, len(documents))
	for _, document := range documents {
		names = append(names, document.name)
	}
	if err := rejectInstallPathAliases(root, names); err != nil {
		return err
	}
	files, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, files.Close()) }()
	backup, err := os.MkdirTemp(filepath.Join(root, ".lucy"), "rollback-*")
	if err != nil {
		return err
	}
	backupRelative := filepath.ToSlash(filepath.Join(".lucy", filepath.Base(backup)))
	journalPublished := false
	defer func() {
		if journalPublished {
			return
		}
		if err := os.RemoveAll(backup); err != nil {
			logger.Warn(fmt.Errorf("remove unused adoption backup: %w", err))
		}
	}()
	data := make(map[string][]byte, len(documents))
	journal := workspacewrite.InstallJournal{
		Backup:  backupRelative,
		Entries: make([]workspacewrite.InstallJournalEntry, 0, len(documents)),
	}
	for _, document := range documents {
		if err := workspacewrite.SafePath(root, document.name); err != nil {
			return err
		}
		target := filepath.Join(root, filepath.FromSlash(document.name))
		before, err := observeInstallPath(files, document.name)
		if err != nil {
			return err
		}
		if !workspacewrite.InstallImagesEqual(before, document.plan.Base) {
			return fmt.Errorf(
				"adoption document %s changed while planning",
				document.name,
			)
		}
		after := &document.plan.Image
		if workspacewrite.InstallImagesEqual(before, after) {
			continue
		}
		if before != nil {
			if err := preserveInstallPreImage(
				files,
				backup,
				backupRelative,
				*before,
				target,
			); err != nil {
				return err
			}
		}
		journal.Entries = append(journal.Entries, workspacewrite.InstallJournalEntry{
			Path:   document.name,
			Before: before,
			After:  after,
		})
		data[document.name] = document.plan.Data
	}
	if len(journal.Entries) == 0 {
		return nil
	}
	journalBytes, err := yaml.Marshal(journal)
	if err != nil {
		return err
	}
	journalPath := filepath.Join(root, workspacewrite.InstallJournalName)
	if err := fsutil.AtomicWrite(journalPath, journalBytes, 0o600); err != nil {
		// AtomicWrite can return a sync error after the replacement was
		// published. Keep the backup while any journal remains so recovery can
		// verify and remove it.
		if _, statErr := os.Lstat(journalPath); statErr == nil {
			journalPublished = true
		}
		return err
	}
	journalPublished = true
	logger.Debug(
		"adoption metadata journaled",
		"journal", journalPath,
		"backup", journal.Backup,
		"paths", len(journal.Entries),
	)
	failed := true
	defer func() {
		if !failed {
			return
		}
		committed, _ := workspacewrite.InstallJournalCommitted(root)
		resultErr = errors.Join(resultErr, workspaceguard.RecoverInstall(root, logger))
		if committed && resultErr != nil {
			resultErr = &CommittedError{Err: fmt.Errorf(
				"adoption committed; %w", resultErr,
			)}
		}
	}()
	for _, entry := range journal.Entries {
		if err := publishInstallBytes(
			files,
			entry.Path,
			data[entry.Path],
			entry.After,
			entry,
		); err != nil {
			return err
		}
	}
	if err := verifyAdoptedOutputs(root, expected); err != nil {
		return err
	}
	for _, entry := range journal.Entries {
		current, err := observeInstallPath(files, entry.Path)
		if err != nil {
			return err
		}
		if !installFinalImageMatches(current, entry.After, true) {
			return fmt.Errorf(
				"adoption document %s changed before commit",
				entry.Path,
			)
		}
	}
	// Documents the transaction reuses are read-only commit preconditions:
	// recheck them alongside the rewritten documents immediately before the
	// commit point, so an external edit cannot leave the published manifest
	// and receipt describing a different lock.
	rewritten := make(map[string]struct{}, len(journal.Entries))
	for _, entry := range journal.Entries {
		rewritten[entry.Path] = struct{}{}
	}
	for _, pin := range protected {
		if _, ok := rewritten[pin.name]; ok {
			continue
		}
		current, err := observeInstallPath(files, pin.name)
		if err != nil {
			return err
		}
		if !workspacewrite.InstallImagesEqual(current, pin.image) {
			return fmt.Errorf(
				"adoption document %s changed before commit; refusing to publish metadata that describes a different workspace",
				pin.name,
			)
		}
	}
	journal.Committed = true
	committedBytes, err := yaml.Marshal(journal)
	if err != nil {
		return err
	}
	if err := fsutil.AtomicWrite(journalPath, committedBytes, 0o600); err != nil {
		return err
	}
	failed = false
	if err := os.RemoveAll(backup); err != nil {
		return &CommittedError{Err: fmt.Errorf(
			"adoption committed; remove rollback backup %s: %w",
			backup, err,
		)}
	}
	if err := workspacewrite.RemoveRoot(files, workspacewrite.InstallJournalName); err != nil {
		return &CommittedError{Err: fmt.Errorf(
			"adoption committed; remove transaction journal: %w", err,
		)}
	}
	logger.Debug("adoption metadata committed", "root", root, "paths", len(journal.Entries))
	return nil
}

// verifyAdoptedOutputs rechecks the files adoption observed. A present file
// that changed, disappeared, or changed mode after planning must not enter the
// receipt with a stale baseline. A file that was already missing keeps its
// ownership record for later repair; a path that reappears is not treated as
// newly observed evidence.
func verifyAdoptedOutputs(root string, expected []adoptionExpectation) error {
	for _, want := range expected {
		entry := want.entry
		if err := workspacewrite.SafePath(root, entry.Path); err != nil {
			return err
		}
		target := filepath.Join(root, filepath.FromSlash(entry.Path))
		info, err := os.Lstat(target)
		if errors.Is(err, os.ErrNotExist) {
			if !want.present {
				continue
			}
			return fmt.Errorf(
				"adopted output %s disappeared before publication",
				entry.Path,
			)
		}
		if err != nil {
			return err
		}
		if !want.present {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf(
				"adopted output %s is no longer a regular file",
				entry.Path,
			)
		}
		hashes, err := DigestFile(target, nil)
		if err != nil {
			return err
		}
		if !strings.EqualFold(hashes["sha256"], entry.SHA256) {
			return fmt.Errorf(
				"adopted output %s changed before publication",
				entry.Path,
			)
		}
		if entry.Mode != 0 && uint32(info.Mode().Perm()) != entry.Mode {
			return fmt.Errorf(
				"adopted output %s changed mode before publication",
				entry.Path,
			)
		}
	}
	return nil
}
