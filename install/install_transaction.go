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
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/receipt"
)

type installCommitMode int

const (
	// installCommitReconcile applies a staged tree, updates both
	// fingerprints, and prunes outputs the resolution no longer needs.
	installCommitReconcile installCommitMode = iota
	// installCommitMetadataOnly publishes documents and carries the accepted
	// ownership ledger forward without walking a stage or pruning.
	installCommitMetadataOnly
	// installCommitOverlay applies staged paths over the existing ledger,
	// preserving entries the stage does not mention. Wrapper reconciliation
	// uses it so an unchanged bootstrap and package set is never pruned.
	installCommitOverlay
)

// stagedOutputPreserved reports whether a staged classification leaves any
// existing workspace file user-owned. Seeds, discard material, and
// bootstrap-only tools never grant pruning authority.
func stagedOutputPreserved(out bootstrap.Output) bool {
	return out.Policy == bootstrap.PolicySeed ||
		out.Policy == bootstrap.PolicyDiscard ||
		out.Role == bootstrap.RoleBootstrapOnly
}

// prunable reports whether reconcile may remove an accepted entry: the file
// was not restaged, is not tracked, carries no preserved policy, and belongs
// to an origin this commit governs.
func prunable(
	entry receipt.Entry,
	tracked map[string]bool,
	staged map[string]commitWrite,
	stagedOutputs map[string]bootstrap.Output,
	stagedBootstrap bool,
) (bool, error) {
	if tracked[strings.ToLower(entry.Path)] {
		return false, nil
	}
	if _, restaged := staged[entry.Path]; restaged {
		return false, nil
	}
	if out, classified := stagedOutputs[entry.Path]; classified &&
		stagedOutputPreserved(out) {
		return false, nil
	}
	switch entry.Origin {
	case receipt.OriginBootstrap:
		return stagedBootstrap, nil
	case receipt.OriginPackage, receipt.OriginMCDR:
		return true, nil
	default:
		return false, fmt.Errorf(
			"unrecognized entry origin %q for path %s; refusing implicit prune",
			entry.Origin, entry.Path,
		)
	}
}

// pruneTargets plans the accepted entries a reconcile removes. An entry whose
// file is already absent maps to nil. That absence stays a precondition: the
// journal plan and the pre-commit check fail if the path reappears, so a file
// that appears after planning is never silently dropped from the ledger. A
// present entry maps to the image observed now, which later steps recheck
// before the journal captures its before image.
func pruneTargets(
	root string,
	previous []receipt.Entry,
	tracked map[string]bool,
	staged map[string]commitWrite,
	stagedOutputs map[string]bootstrap.Output,
	stagedBootstrap bool,
) (map[string]*workspacewrite.InstallImage, error) {
	files, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer files.Close()
	targets := make(map[string]*workspacewrite.InstallImage)
	for _, old := range previous {
		shouldPrune, err := prunable(
			old,
			tracked,
			staged,
			stagedOutputs,
			stagedBootstrap,
		)
		if err != nil {
			return nil, err
		}
		if !shouldPrune {
			continue
		}
		current, err := workspacewrite.ObserveInstallImage(files, old.Path)
		if err != nil {
			if info, statErr := files.Lstat(old.Path); statErr == nil &&
				!info.Mode().IsRegular() {
				return nil, fmt.Errorf("owned output is not a regular file: %s", old.Path)
			}
			return nil, err
		}
		if current == nil {
			targets[old.Path] = nil
			continue
		}
		if !matchesReceipt(old, current) {
			return nil, fmt.Errorf("modified owned output cannot be pruned: %s", old.Path)
		}
		targets[old.Path] = current
	}
	return targets, nil
}

// applyCommit publishes the staged installation against documents the caller
// observed before the operation read or resolved anything, so an edit made
// after observation fails the transaction instead of being overwritten.
// stagedOutputs classifies every non-package file the stage is allowed to
// contain, including method bootstrap outputs validated before packages joined
// the stage and explicit wrapper outputs. mcdrOutputs identifies the subset
// produced by the MCDReforged wrapper so those entries carry their own receipt
// origin. stagedBootstrap reports whether the bootstrap contract ran for this
// operation; when false its established outputs are preserved and revalidated
// against the pinned receipt, so drift between the currency check and the
// commit aborts before anything is written. The commit never inspects the stage
// tree against the bootstrap contract, because that tree also contains package
// and wrapper files that are not bootstrap outputs.
func applyCommit(root, stage string, d *document.Manifest, l *document.Lock, publish bool, overwritePaths map[string]bool, stagedOutputs, mcdrOutputs map[string]bootstrap.Output, stagedBootstrap bool, mode installCommitMode, documents WorkspaceDocuments, logger *log.Logger) (FileChanges, error) {
	reconcile := mode == installCommitReconcile
	metadataOnly := mode == installCommitMetadataOnly
	previous := receipt.New()
	receiptPin, ok := documents.documents[receipt.Filename]
	if !ok {
		return FileChanges{}, fmt.Errorf("installation documents do not capture %s", receipt.Filename)
	}
	if receiptPin.image != nil {
		parsed, err := receipt.Decode(bytes.NewReader(receiptPin.raw))
		if err != nil {
			return FileChanges{}, err
		}
		previous = *parsed
	}
	if !stagedBootstrap {
		// The currency check that skipped bootstrap staging predates
		// package staging and downloads, so the outputs this commit
		// retains are revalidated here: a fingerprint carried forward
		// over drifted files would leave the receipt reporting a server
		// tree that no longer satisfies the contract.
		retained, err := bootstrapCurrent(root, l, &previous, logger)
		if err != nil {
			return FileChanges{}, err
		}
		if !retained {
			return FileChanges{}, fmt.Errorf(
				"retained bootstrap outputs no longer satisfy the locked bootstrap contract; rerun the install to restage the server tree",
			)
		}
	}
	owned := map[string]receipt.Entry{}
	for _, entry := range previous.Entries {
		owned[entry.Path] = entry
	}
	next := receipt.New()
	nextIndex := make(map[string]int)
	packageByDest := make(map[string]string)
	if reconcile {
		bf, err := receipt.BootstrapFingerprint(l)
		if err != nil {
			return FileChanges{}, err
		}
		pf, err := receipt.PackageFingerprint(l)
		if err != nil {
			return FileChanges{}, err
		}
		if stagedBootstrap {
			next.BootstrapFingerprint = bf
		} else {
			next.BootstrapFingerprint = previous.BootstrapFingerprint
		}
		next.PackageFingerprint = pf
		if !stagedBootstrap {
			for _, entry := range previous.Entries {
				if entry.Origin != receipt.OriginBootstrap {
					continue
				}
				if out, classified := stagedOutputs[entry.Path]; classified &&
					stagedOutputPreserved(out) {
					continue
				}
				next.Entries = append(next.Entries, entry)
			}
		}
	} else {
		// Metadata-only publication carries the accepted ownership ledger
		// forward untouched. Overlay reconciliation updates only the staged
		// paths and preserves every other accepted entry.
		next.BootstrapFingerprint = previous.BootstrapFingerprint
		next.PackageFingerprint = previous.PackageFingerprint
		next.Entries = slices.Clone(previous.Entries)
	}
	if !metadataOnly {
		// A preserved bootstrap entry can also be restaged, for example a
		// wrapper regenerated on every package change. Merge by path so the
		// restaged bytes update the accepted baseline instead of producing a
		// duplicate entry, and refuse when the staged file claims a different
		// origin.
		for i, entry := range next.Entries {
			nextIndex[entry.Path] = i
		}
		for i := range l.Packages {
			p := &l.Packages[i]
			if !p.Enabled() {
				continue
			}
			dest, err := l.PackageDestination(p)
			if err != nil {
				continue
			}
			clean := filepath.ToSlash(filepath.Clean(dest))
			packageByDest[clean] = p.Artifact.Provider.String() + ":" + p.Artifact.ProjectID
		}
	}
	tracked := trackedPaths(l)
	staged := make(map[string]commitWrite)
	err := func() error {
		if metadataOnly {
			return nil
		}
		return filepath.WalkDir(stage, func(p string, e os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if e.IsDir() {
				return nil
			}
			relative, err := filepath.Rel(stage, p)
			if err != nil {
				return err
			}
			relative = filepath.ToSlash(relative)
			if err := document.SafeRelativePath(relative); err != nil {
				return err
			}
			out, classified := stagedOutputs[relative]
			if classified && (out.Policy == bootstrap.PolicyDiscard || out.Role == bootstrap.RoleBootstrapOnly) {
				return nil
			}
			packageID, isPackage := packageByDest[relative]
			if classified && isPackage {
				return fmt.Errorf("staged output %s has both bootstrap and package origins", relative)
			}
			if !classified && !isPackage {
				return fmt.Errorf("staged output %s is not classified by the bootstrap or package plan", relative)
			}
			target := filepath.Join(root, relative)
			isSeed := classified && out.Policy == bootstrap.PolicySeed
			if tracked[strings.ToLower(relative)] {
				if !isSeed {
					return fmt.Errorf("tracked file %s collides with managed runtime output", relative)
				}
				return nil
			}
			if isSeed {
				if _, err := os.Stat(target); err == nil {
					return nil
				}
			}
			sha256 := out.SHA256
			if sha256 == "" {
				hashes, err := DigestFile(p, nil)
				if err != nil {
					return err
				}
				sha256 = hashes["sha256"]
			}
			info, err := e.Info()
			if err != nil {
				return err
			}
			fileMode := os.FileMode(0o644)
			if !isSeed {
				fileMode = info.Mode().Perm()
			}
			var base *workspacewrite.InstallImage
			targetInfo, err := os.Lstat(target)
			switch {
			case err == nil:
				if targetInfo.Mode()&os.ModeSymlink != 0 {
					return fmt.Errorf("refuse symlink output %s", relative)
				}
				if !targetInfo.Mode().IsRegular() {
					return fmt.Errorf("refuse non-regular output %s", relative)
				}
				actual, err := DigestFile(target, nil)
				if err != nil {
					return err
				}
				current := workspacewrite.InstallImage{
					SHA256: actual["sha256"],
					Mode:   workspacewrite.EffectiveInstallMode(targetInfo.Mode().Perm()),
				}
				base = &current
				prior, isOwned := owned[relative]
				if !isOwned {
					switch {
					case overwritePaths[relative]:
						logger.Debug(
							"overwriting unowned output",
							"path", relative,
							"reason", "overwrite requested",
						)
					case classified && !isPackage && strings.EqualFold(actual["sha256"], sha256):
						// Retain the existing mode; adoption is not a replacement.
						fileMode = targetInfo.Mode().Perm()
						logger.Debug(
							"adopting byte-identical unowned output",
							"path", relative,
							"sha256", actual["sha256"],
						)
					default:
						if isPackage {
							return fmt.Errorf("unowned output conflict: %s (rerun with --overwrite to replace it)", relative)
						}
						return fmt.Errorf(
							"unowned output conflict: %s (move the conflicting file before retrying bootstrap)",
							relative,
						)
					}
				} else if !matchesReceipt(prior, &current) {
					if overwritePaths[relative] {
						logger.Debug(
							"overwriting modified output",
							"path", relative,
							"reason", "overwrite requested",
						)
					} else {
						return fmt.Errorf("modified owned output conflict: %s", relative)
					}
				}
			case errors.Is(err, os.ErrNotExist):
				// The write's base stays nil so the planner records an absent
				// precondition instead of dropping the path.
			default:
				return err
			}
			if !isSeed {
				origin := receipt.OriginBootstrap
				if _, isMCDR := mcdrOutputs[relative]; isMCDR {
					origin = receipt.OriginMCDR
				}
				if isPackage {
					if origin != receipt.OriginBootstrap {
						return fmt.Errorf(
							"staged output %s has both MCDR and package origins",
							relative,
						)
					}
					origin = receipt.OriginPackage
				}
				entry := receipt.Entry{
					Path:    relative,
					SHA256:  sha256,
					Mode:    uint32(fileMode),
					Origin:  origin,
					Package: packageID,
				}
				if prior, exists := owned[relative]; exists && prior.Origin != entry.Origin {
					return fmt.Errorf(
						"staged output %s changes receipt origin from %s to %s",
						relative, prior.Origin, entry.Origin,
					)
				}
				if index, exists := nextIndex[relative]; exists {
					prior := next.Entries[index]
					if prior.Origin != entry.Origin {
						return fmt.Errorf(
							"staged output %s changes receipt origin from %s to %s",
							relative, prior.Origin, entry.Origin,
						)
					}
					next.Entries[index] = entry
				} else {
					nextIndex[relative] = len(next.Entries)
					next.Entries = append(next.Entries, entry)
				}
			}
			after := workspacewrite.InstallImage{
				SHA256: sha256,
				Mode:   workspacewrite.EffectiveInstallMode(fileMode),
			}
			staged[relative] = commitWrite{
				path:   relative,
				source: p,
				base:   base,
				after:  &after,
			}
			return nil
		})
	}()
	if err != nil {
		return FileChanges{}, err
	}
	var pruneExpected map[string]*workspacewrite.InstallImage
	if reconcile {
		pruneExpected, err = pruneTargets(
			root,
			previous.Entries,
			tracked,
			staged,
			stagedOutputs,
			stagedBootstrap,
		)
		if err != nil {
			return FileChanges{}, err
		}
	}
	documentWrites, err := documents.documentWrites(
		documentCandidate{
			name: document.LockFilename, mode: 0o644, publish: true,
			snapshot: func() ([]byte, error) { return document.LockFile(root).Snapshot(l) },
			merge:    l.MergeBytes,
		},
		documentCandidate{
			name: receipt.Filename, mode: 0o600, publish: true,
			snapshot: next.Snapshot,
		},
		documentCandidate{
			name: document.ManifestFilename, mode: 0o644, publish: publish,
			snapshot: func() ([]byte, error) { return document.ManifestFile(root).Snapshot(d) },
			merge:    d.MergeBytes,
		},
	)
	if err != nil {
		return FileChanges{}, err
	}
	writes := make([]commitWrite, 0, len(staged)+len(documentWrites))
	for name, write := range staged {
		if installPathReserved(root, name) {
			return FileChanges{}, fmt.Errorf("staged output %s is reserved", name)
		}
		writes = append(writes, write)
	}
	writes = append(writes, documentWrites...)
	prunes := make([]commitPrune, 0, len(pruneExpected))
	for name, expected := range pruneExpected {
		if installPathMatchesReserved(root, name, document.ManifestFilename) {
			return FileChanges{}, fmt.Errorf("installation receipt path %s is reserved", name)
		}
		prunes = append(prunes, commitPrune{path: name, expected: expected})
	}
	// Paths the executor does not journal are skipped by its own recheck, so
	// compare them here against the images the plan expects.
	verify := func(
		files *os.Root,
		entries []workspacewrite.InstallJournalEntry,
	) error {
		mutated := make(map[string]struct{}, len(entries))
		for _, entry := range entries {
			mutated[entry.Path] = struct{}{}
		}
		check := func(name string, expected *workspacewrite.InstallImage) error {
			if _, changed := mutated[name]; changed {
				return nil
			}
			current, err := workspacewrite.ObserveInstallImage(files, name)
			if err != nil {
				return err
			}
			if !workspacewrite.InstallImagesEqual(current, expected) {
				return fmt.Errorf(
					"installation path %s changed before commit",
					name,
				)
			}
			return nil
		}
		for name, write := range staged {
			if err := check(name, write.after); err != nil {
				return err
			}
		}
		for name, expected := range pruneExpected {
			if err := check(name, expected); err != nil {
				return err
			}
		}
		return nil
	}
	applied, err := executeCommit(root, commitRequest{
		label:     "installation",
		writes:    writes,
		prunes:    prunes,
		documents: documents,
		verify:    verify,
	}, logger)
	if err != nil {
		return FileChanges{}, err
	}
	var counts FileChanges
	for _, entry := range applied {
		if entry.After == nil {
			if _, pruned := pruneExpected[entry.Path]; pruned {
				counts.Removed++
			}
			continue
		}
		if _, restaged := staged[entry.Path]; !restaged {
			continue
		}
		if entry.Before == nil {
			counts.Created++
		} else if entry.Before.SHA256 != entry.After.SHA256 {
			counts.Replaced++
		}
	}
	logger.Info(
		"installation committed",
		"root", root,
		"files", len(staged),
		"pruned", counts.Removed,
		"manifest-published", publish,
	)
	return counts, nil
}
