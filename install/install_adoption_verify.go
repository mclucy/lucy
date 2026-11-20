package install

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/bootstrap"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/receipt"
)

// receiptImage returns the content and normalized permission mode an accepted
// receipt entry records. A zero mode predates mode recording, so callers match
// it without permission evidence.
func receiptImage(entry receipt.Entry) *workspacewrite.InstallImage {
	return &workspacewrite.InstallImage{
		SHA256: strings.ToLower(entry.SHA256),
		Mode:   workspacewrite.EffectiveInstallMode(os.FileMode(entry.Mode)),
	}
}

// matchesReceipt reports whether current still satisfies an accepted receipt
// entry. A zero mode is a legacy wildcard; every other mode is exact.
func matchesReceipt(entry receipt.Entry, current *workspacewrite.InstallImage) bool {
	return workspacewrite.InstallImagesMatch(
		current,
		receiptImage(entry),
		entry.Mode != 0,
	)
}

// verifyExistingBaseline checks that a receipt entry still describes the file
// it protects and returns the observed image. Content and permission changes
// fail closed; a missing file returns a nil image and keeps its ownership
// record for later repair.
func verifyExistingBaseline(
	files *os.Root,
	cleanPath string,
	entry receipt.Entry,
) (*workspacewrite.InstallImage, error) {
	current, err := workspacewrite.ObserveInstallImage(files, cleanPath)
	if err != nil {
		return nil, fmt.Errorf("inspect existing managed path %s: %w", cleanPath, err)
	}
	if current == nil {
		return nil, nil
	}
	if entry.SHA256 == "" {
		return nil, fmt.Errorf(
			"existing managed path %s has no accepted baseline; remove its receipt entry before re-initializing",
			cleanPath,
		)
	}
	if matchesReceipt(entry, current) {
		return current, nil
	}
	if !strings.EqualFold(current.SHA256, entry.SHA256) {
		return nil, fmt.Errorf(
			"locally modified managed output %s cannot be re-initialized without explicit conflict resolution",
			cleanPath,
		)
	}
	return nil, fmt.Errorf(
		"managed output %s changed mode from %#o to %#o; resolve the modification before re-initializing",
		cleanPath, entry.Mode, current.Mode,
	)
}

// managedRuntimeScope reports whether serverRel falls inside a managed runtime
// output declared by the locked method. Scope matching stops at a path
// component boundary, so "libraries/net" never authorizes
// "libraries/networking.jar".
func managedRuntimeScope(specs []bootstrap.OutputSpec, serverRel string) bool {
	serverRel = cleanWorkspacePath(serverRel)
	for _, spec := range specs {
		if spec.Policy != bootstrap.PolicyManaged ||
			spec.Role != bootstrap.RoleRuntime {
			continue
		}
		if spec.Path != "" {
			if serverRel == cleanWorkspacePath(spec.Path) {
				return true
			}
			continue
		}
		scope := cleanWorkspacePath(spec.Scope)
		if scope == "." || scope == "" {
			continue
		}
		if serverRel == scope || strings.HasPrefix(serverRel, scope+"/") {
			return true
		}
	}
	return false
}

// adoptionReceiptChanged reports whether adoption must rewrite the receipt.
// An unchanged receipt keeps its exact bytes, including hand-formatted fields;
// a legacy receipt always needs the modern origins Snapshot emits.
func adoptionReceiptChanged(
	previous *receipt.Receipt,
	legacy bool,
	next receipt.Receipt,
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
	if len(previous.Entries) != len(next.Entries) {
		return true
	}
	prior := make(map[string]receipt.Entry, len(previous.Entries))
	for _, entry := range previous.Entries {
		prior[entry.Path] = entry
	}
	for _, entry := range next.Entries {
		if prior[entry.Path] != entry {
			return true
		}
	}
	return false
}

// packageSetObserved reports whether the adopted entries already describe the
// lock's enabled packages, so the receipt may record its package fingerprint.
// Package-owned paths that the lock no longer selects refuse the fingerprint
// because they still need pruning.
func packageSetObserved(
	root string,
	lock *document.Lock,
	entries map[string]receipt.Entry,
) bool {
	if len(lock.Packages) == 0 && lock.MCDR != nil {
		return false
	}
	claimed := make(map[string]struct{}, len(lock.Packages))
	for i := range lock.Packages {
		pkg := &lock.Packages[i]
		if !pkg.Enabled() {
			continue
		}
		dest, err := lock.PackageDestination(pkg)
		if err != nil {
			return false
		}
		cleanDest := cleanWorkspacePath(dest)
		entry, ok := entries[cleanDest]
		if !ok || entry.Origin != receipt.OriginPackage {
			return false
		}
		expectedID := pkg.Artifact.Provider.String() + ":" + pkg.Artifact.ProjectID
		if pkg.Artifact.ProjectID != "" && entry.Package != "" &&
			entry.Package != expectedID && entry.Package != pkg.Filename {
			return false
		}
		if len(pkg.Artifact.Hashes) == 0 {
			return false
		}
		target := filepath.Join(root, filepath.FromSlash(cleanDest))
		if _, err := DigestFile(target, pkg.Artifact.Hashes); err != nil {
			return false
		}
		claimed[cleanDest] = struct{}{}
	}
	for path, entry := range entries {
		switch entry.Origin {
		case receipt.OriginPackage:
			if _, ok := claimed[path]; !ok {
				return false
			}
		case receipt.OriginMCDR:
			if lock.MCDR == nil {
				return false
			}
		}
	}
	return true
}

// verifyAdoptedOutputs rechecks the exact images adoption observed. A present
// file that changed, disappeared, or changed mode after planning must not
// enter the receipt with a stale baseline. A file that was already missing
// keeps its ownership record for later repair, but a path that reappears
// before the commit point is a conflict, not observed evidence.
func verifyAdoptedOutputs(
	files *os.Root,
	root string,
	expected map[string]*workspacewrite.InstallImage,
) error {
	for path, want := range expected {
		if err := workspacewrite.SafePath(root, path); err != nil {
			return err
		}
		current, err := workspacewrite.ObserveInstallImage(files, path)
		if err != nil {
			return fmt.Errorf("inspect adopted output %s before publication: %w", path, err)
		}
		switch {
		case want == nil:
			if current != nil {
				return fmt.Errorf(
					"adopted output %s appeared before publication",
					path,
				)
			}
		case current == nil:
			return fmt.Errorf(
				"adopted output %s disappeared before publication",
				path,
			)
		case workspacewrite.InstallImagesEqual(current, want):
		case !strings.EqualFold(current.SHA256, want.SHA256):
			return fmt.Errorf(
				"adopted output %s changed before publication",
				path,
			)
		default:
			return fmt.Errorf(
				"adopted output %s changed mode before publication",
				path,
			)
		}
	}
	return nil
}
