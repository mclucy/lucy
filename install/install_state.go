package install

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/bootstrap"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/receipt"
)

// installationCurrent reports whether the receipt answers the lock and both
// bootstrap and package convergence are current.
func installationCurrent(root string, l *document.Lock, logger *log.Logger) (bool, error) {
	current, err := receipt.Read(root)
	if errors.Is(err, os.ErrNotExist) {
		logger.Debug("installation not current", "reason", "no install receipt")
		return false, nil
	}
	if err != nil {
		return false, err
	}
	bCurrent, err := bootstrapCurrent(root, l, current, logger)
	if err != nil || !bCurrent {
		return false, err
	}
	pCurrent, err := packagesCurrent(root, l, current, logger)
	if err != nil || !pCurrent {
		return false, err
	}
	logger.Debug(
		"installation current",
		"reason", "receipt satisfies bootstrap contract and package set",
		"receipt", filepath.Join(root, receipt.Filename),
	)
	return true, nil
}

// bootstrapCurrent reports whether the receipt satisfies the resolved bootstrap
// plan: every required managed bootstrap output is owned with bootstrap origin
// and still matches its accepted baseline.
func bootstrapCurrent(root string, l *document.Lock, current *receipt.Receipt, logger *log.Logger) (bool, error) {
	if current == nil {
		return false, nil
	}
	if current.BootstrapFingerprint == "" {
		logger.Debug("bootstrap not current", "reason", "bootstrap fingerprint unproven or missing")
		return false, nil
	}
	expected, err := receipt.BootstrapFingerprint(l)
	if err != nil {
		return false, err
	}
	if current.BootstrapFingerprint != expected {
		logger.Debug(
			"bootstrap not current",
			"reason", "bootstrap fingerprint changed",
			"receipt", current.BootstrapFingerprint,
			"expected", expected,
		)
		return false, nil
	}

	specs := bootstrap.OutputSpecs(l.Server)
	if len(specs) == 0 && (l.Server.Product != "" || l.Server.Bootstrapper != "") {
		logger.Debug("bootstrap not current", "reason", "unsupported bootstrap method", "method", l.Server.Bootstrapper)
		return false, nil
	}

	serverDir := filepath.Join(root, filepath.FromSlash(l.Server.Directory))
	checkResult, err := bootstrap.CheckExisting(serverDir, l.Server)
	if err != nil {
		return false, err
	}
	switch checkResult.Status {
	case bootstrap.CurrentnessCurrent:
	case bootstrap.CurrentnessMismatch:
		logger.Debug("bootstrap not current", "reason", checkResult.Reason)
		return false, nil
	case bootstrap.CurrentnessInconclusive:
		for _, seedPath := range bootstrap.RequiredSeedOutputs(l.Server) {
			p := filepath.Join(serverDir, filepath.FromSlash(seedPath))
			if _, err := os.Stat(p); err != nil {
				logger.Debug("bootstrap not current", "reason", "required launch seed missing", "path", seedPath)
				return false, nil
			}
		}
	}

	tracked := trackedPaths(l)
	owned := make(map[string]bool)
	hasBootstrapEntry := false
	for _, entry := range current.Entries {
		if entry.Origin != receipt.OriginBootstrap {
			continue
		}
		hasBootstrapEntry = true
		if tracked[strings.ToLower(entry.Path)] {
			continue
		}
		owned[entry.Path] = true
		entryCurrent, err := receiptEntryCurrent(root, entry, "bootstrap", logger)
		if err != nil || !entryCurrent {
			return false, err
		}
	}
	if l.Server.Product != "" && !hasBootstrapEntry {
		logger.Debug("bootstrap not current", "reason", "no bootstrap entries recorded in receipt")
		return false, nil
	}
	for _, required := range requiredBootstrapOutputs(l.Server, specs, checkResult.Outputs) {
		if tracked[strings.ToLower(required)] {
			continue
		}
		if !owned[required] {
			logger.Debug("bootstrap not current", "reason", "required managed output not owned by receipt", "path", required)
			return false, nil
		}
	}
	return true, nil
}

// requiredBootstrapOutputs lists the workspace paths the receipt must own with
// bootstrap origin: every managed exact output the contract declares and every
// managed output CheckExisting verified, including dynamically attributed
// ones. Seed, discard, and bootstrap-only material carries no such requirement.
func requiredBootstrapOutputs(
	locked document.LockedServer,
	specs []bootstrap.OutputSpec,
	outputs []bootstrap.Output,
) []string {
	var required []string
	for _, spec := range specs {
		if spec.Path == "" || spec.Policy != bootstrap.PolicyManaged ||
			spec.Role == bootstrap.RoleBootstrapOnly {
			continue
		}
		required = append(required, filepath.ToSlash(filepath.Join(locked.Directory, spec.Path)))
	}
	for _, out := range outputs {
		if out.Policy == bootstrap.PolicyManaged && out.Role != bootstrap.RoleBootstrapOnly {
			required = append(required, out.Path)
		}
	}
	return required
}

// packagesCurrent reports whether recorded package entries match the lock's
// package pins and every package file exists with its expected digest.
func packagesCurrent(root string, l *document.Lock, current *receipt.Receipt, logger *log.Logger) (bool, error) {
	if current == nil {
		return false, nil
	}
	expected, err := receipt.PackageFingerprint(l)
	if err != nil {
		return false, err
	}
	if current.PackageFingerprint != expected {
		logger.Debug(
			"packages not current",
			"reason", "package fingerprint changed",
			"receipt", current.PackageFingerprint,
			"expected", expected,
		)
		return false, nil
	}

	recordedPackages := make(map[string]receipt.Entry, len(current.Entries))
	for _, entry := range current.Entries {
		if entry.Origin == receipt.OriginPackage {
			recordedPackages[filepath.ToSlash(filepath.Clean(filepath.FromSlash(entry.Path)))] = entry
		}
	}

	for i := range l.Packages {
		p := &l.Packages[i]
		if !p.Enabled() {
			continue
		}
		dest, err := l.PackageDestination(p)
		if err != nil {
			return false, err
		}
		cleanDest := filepath.ToSlash(filepath.Clean(filepath.FromSlash(dest)))
		entry, recorded := recordedPackages[cleanDest]
		if !recorded {
			logger.Debug("packages not current", "reason", "locked package not recorded in receipt", "path", cleanDest)
			return false, nil
		}
		if entry.Package != p.Artifact.Provider.String()+":"+p.Artifact.ProjectID {
			logger.Debug("packages not current", "reason", "package identity mismatch", "path", cleanDest)
			return false, nil
		}
		current, err := receiptEntryCurrent(root, entry, "packages", logger)
		if err != nil || !current {
			return false, err
		}
	}
	return true, nil
}

// receiptEntryCurrent reports whether the file a receipt entry protects still
// exists as a regular file with its accepted digest and mode. kind names the
// entry class in diagnostics.
func receiptEntryCurrent(
	root string,
	entry receipt.Entry,
	kind string,
	logger *log.Logger,
) (bool, error) {
	if err := workspacewrite.SafePath(root, entry.Path); err != nil {
		return false, err
	}
	target := filepath.Join(root, filepath.FromSlash(entry.Path))
	info, err := os.Lstat(target)
	switch {
	case os.IsNotExist(err):
		logger.Debug(kind+" not current", "reason", "managed output missing", "path", entry.Path)
		return false, nil
	case err != nil:
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		logger.Debug(kind+" not current", "reason", "managed output is symlink or non-regular", "path", entry.Path)
		return false, nil
	}
	actual, err := DigestFile(target, nil)
	if err != nil {
		return false, err
	}
	if !strings.EqualFold(actual["sha256"], entry.SHA256) {
		logger.Debug(kind+" not current", "reason", "managed output modified", "path", entry.Path, "baseline", entry.SHA256, "actual", actual["sha256"])
		return false, nil
	}
	if entry.Mode != 0 {
		actualMode := effectiveInstallMode(info.Mode().Perm())
		expectedMode := effectiveInstallMode(os.FileMode(entry.Mode))
		if actualMode != expectedMode {
			logger.Debug(kind+" not current", "reason", "managed output mode modified", "path", entry.Path, "expected", expectedMode, "actual", actualMode)
			return false, nil
		}
	}
	return true, nil
}

// trackedPaths lowercases the lock's tracked-file paths, because the
// filesystems that host a workspace may compare them case-insensitively.
func trackedPaths(l *document.Lock) map[string]bool {
	paths := make(map[string]bool, len(l.Files))
	for _, file := range l.Files {
		paths[strings.ToLower(file.Path)] = true
	}
	return paths
}
