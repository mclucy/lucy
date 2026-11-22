package install

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/fileidentity"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/receipt"
)

// currentCheck carries the digest evidence of one command. memo avoids a
// second read of a file this command already hashed, and evidence answers a
// check from stat identities an earlier command recorded.
type currentCheck struct {
	memo     *fileidentity.Memo
	evidence *currentEvidence
}

// installationCurrent reports whether the receipt answers the lock and both
// bootstrap and package convergence are current. It returns the evidence it
// collected so a caller can record the identities this command proved.
func installationCurrent(
	ctx context.Context,
	root string,
	l *document.Lock,
	memo *fileidentity.Memo,
	logger *log.Logger,
) (bootstrapOK, packagesOK bool, evidence *currentEvidence, err error) {
	evidence = newCurrentEvidence(root, memo, logger)
	check := currentCheck{memo: memo, evidence: evidence}
	current, err := receipt.Read(root)
	if errors.Is(err, os.ErrNotExist) {
		logger.Debug("installation not current", "reason", "no install receipt")
		return false, false, evidence, nil
	}
	if err != nil {
		return false, false, evidence, err
	}
	bCurrent, err := bootstrapCurrentWithCheck(root, l, current, check, logger)
	if err != nil || !bCurrent {
		return false, false, evidence, err
	}
	pCurrent, err := packagesCurrent(ctx, root, l, current, check, logger)
	if err != nil || !pCurrent {
		return true, false, evidence, err
	}
	logger.Debug(
		"installation current",
		"reason", "receipt satisfies bootstrap contract and package set",
		"receipt", filepath.Join(root, receipt.Filename),
	)
	evidence.write(root, current.Entries, logger)
	return true, true, evidence, nil
}

// bootstrapCurrent reports whether the receipt answers the resolved bootstrap
// plan: its bootstrap fingerprint matches the lock and every recorded bootstrap
// output still matches its accepted baseline.
func bootstrapCurrent(
	root string,
	l *document.Lock,
	current *receipt.Receipt,
	logger *log.Logger,
) (bool, error) {
	return bootstrapCurrentWithCheck(root, l, current, currentCheck{}, logger)
}

func bootstrapCurrentWithCheck(
	root string,
	l *document.Lock,
	current *receipt.Receipt,
	check currentCheck,
	logger *log.Logger,
) (bool, error) {
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

	for _, seed := range current.SeedPaths {
		if err := workspacewrite.SafePath(root, seed); err != nil {
			return false, err
		}
		info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(seed)))
		switch {
		case os.IsNotExist(err):
			logger.Debug("bootstrap not current", "reason", "recorded seed missing", "path", seed)
			return false, nil
		case err != nil:
			return false, err
		}
		if !info.Mode().IsRegular() {
			logger.Debug("bootstrap not current", "reason", "recorded seed is symlink or non-regular", "path", seed)
			return false, nil
		}
	}

	tracked := trackedPaths(l)
	targets := make([]receipt.Entry, 0, len(current.Entries))
	hasBootstrapEntry := false
	for _, entry := range current.Entries {
		if entry.Origin != receipt.OriginBootstrap {
			continue
		}
		hasBootstrapEntry = true
		if tracked[strings.ToLower(entry.Path)] {
			continue
		}
		targets = append(targets, entry)
	}
	if l.Server.Product != "" && !hasBootstrapEntry {
		logger.Debug("bootstrap not current", "reason", "no bootstrap entries recorded in receipt")
		return false, nil
	}
	currentFiles, err := checkFilesInOrder(
		context.Background(),
		len(targets),
		func(i int) (bool, error) {
			return receiptEntryCurrent(root, targets[i], "bootstrap", check, logger)
		},
	)
	return currentFiles, err
}

// packagesCurrent reports whether recorded package entries match the lock's
// package pins and every package file exists with its expected digest.
func packagesCurrent(
	ctx context.Context,
	root string,
	l *document.Lock,
	current *receipt.Receipt,
	check currentCheck,
	logger *log.Logger,
) (bool, error) {
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

	verify := func(i int) (bool, error) {
		p := &l.Packages[i]
		if !p.Enabled() {
			return true, nil
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
		return receiptEntryCurrent(root, entry, "packages", check, logger)
	}
	return checkFilesInOrder(ctx, len(l.Packages), verify)
}

// checkFilesInOrder verifies one file at a time on a bounded pool. Stop
// assigning work on a stale file but finish earlier checks: the first result
// in receipt order decides.
func checkFilesInOrder(
	ctx context.Context,
	count int,
	check func(int) (bool, error),
) (bool, error) {
	var mu sync.Mutex
	var wg sync.WaitGroup
	next, firstFailure := 0, count
	var firstErr error
	for range min(count, 4) {
		wg.Go(func() {
			for ctx.Err() == nil {
				mu.Lock()
				i := next
				if i == count {
					mu.Unlock()
					return
				}
				next++
				mu.Unlock()
				if ok, err := check(i); !ok || err != nil {
					mu.Lock()
					next = count
					if i < firstFailure {
						firstFailure, firstErr = i, err
					}
					mu.Unlock()
					return
				}
			}
		})
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return firstFailure == count, firstErr
}

// receiptEntryCurrent reports whether the file a receipt entry protects still
// exists as a regular file with its accepted digest and mode. kind names the
// entry class in diagnostics. A stat identity the sidecar recorded for the
// accepted digest answers the check without reading the file.
func receiptEntryCurrent(
	root string,
	entry receipt.Entry,
	kind string,
	check currentCheck,
	logger *log.Logger,
) (bool, error) {
	if err := workspacewrite.SafePath(root, entry.Path); err != nil {
		return false, err
	}
	target := filepath.Join(root, filepath.FromSlash(entry.Path))
	info, err := os.Lstat(target)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		logger.Debug(kind+" not current", "reason", "managed output missing", "path", entry.Path)
		return false, nil
	case err != nil:
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		logger.Debug(kind+" not current", "reason", "managed output is symlink or non-regular", "path", entry.Path)
		return false, nil
	}
	if check.evidence.trusted(entry, info) {
		return true, nil
	}
	observation, err := check.memo.ObservePath(target, nil)
	if err != nil {
		return false, err
	}
	actual := observation.Hashes["sha256"]
	if !strings.EqualFold(actual, entry.SHA256) {
		logger.Debug(kind+" not current", "reason", "managed output modified", "path", entry.Path, "baseline", entry.SHA256, "actual", actual)
		return false, nil
	}
	if entry.Mode != 0 {
		actualMode := workspacewrite.EffectiveInstallMode(observation.Mode.Perm())
		expectedMode := workspacewrite.EffectiveInstallMode(os.FileMode(entry.Mode))
		if actualMode != expectedMode {
			logger.Debug(kind+" not current", "reason", "managed output mode modified", "path", entry.Path, "expected", expectedMode, "actual", actualMode)
			return false, nil
		}
	}
	check.evidence.observe(entry, observation.Identity, observation.HasIdentity)
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
