package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/bootstrap"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/receipt"
)

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
