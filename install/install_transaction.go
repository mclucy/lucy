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
	sources map[string]string,
	stagedOutputs map[string]bootstrap.Output,
	stagedBootstrap bool,
) (bool, error) {
	if tracked[strings.ToLower(entry.Path)] {
		return false, nil
	}
	if _, restaged := sources[entry.Path]; restaged {
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
	sources map[string]string,
	stagedOutputs map[string]bootstrap.Output,
	stagedBootstrap bool,
) (map[string]*workspacewrite.InstallImage, error) {
	targets := map[string]*workspacewrite.InstallImage{}
	for _, old := range previous {
		shouldPrune, err := prunable(
			old,
			tracked,
			sources,
			stagedOutputs,
			stagedBootstrap,
		)
		if err != nil {
			return nil, err
		}
		if !shouldPrune {
			continue
		}
		p := filepath.Join(root, old.Path)
		info, err := os.Lstat(p)
		if errors.Is(err, os.ErrNotExist) {
			targets[old.Path] = nil
			continue
		}
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("owned output is not a regular file: %s", old.Path)
		}
		actual, err := DigestFile(p, nil)
		if err != nil {
			return nil, err
		}
		observed := workspacewrite.InstallImage{
			SHA256: actual["sha256"],
			Mode:   workspacewrite.EffectiveInstallMode(info.Mode().Perm()),
		}
		if !strings.EqualFold(observed.SHA256, old.SHA256) {
			return nil, fmt.Errorf("modified owned output cannot be pruned: %s", old.Path)
		}
		if old.Mode != 0 &&
			workspacewrite.EffectiveInstallMode(os.FileMode(old.Mode)) != observed.Mode {
			return nil, fmt.Errorf("modified owned output cannot be pruned: %s", old.Path)
		}
		targets[old.Path] = &observed
	}
	return targets, nil
}

// applyCommit publishes the staged installation. stagedOutputs classifies
// every non-package file the stage is allowed to contain, including method
// bootstrap outputs validated before packages joined the stage and explicit
// wrapper outputs. mcdrOutputs identifies the subset produced by the
// MCDReforged wrapper so those entries carry their own receipt origin.
// stagedBootstrap reports whether the bootstrap contract ran for this
// operation; when false its established outputs are preserved. The commit
// never inspects the stage tree against the bootstrap contract, because that
// tree also contains package and wrapper files that are not bootstrap outputs.
func applyCommit(root, stage string, d *document.Manifest, l *document.Lock, publish bool, overwritePaths map[string]bool, stagedOutputs, mcdrOutputs map[string]bootstrap.Output, stagedBootstrap bool, mode installCommitMode, logger *log.Logger) (FileChanges, error) {
	var counts FileChanges
	resultErr := applyCommitFiles(root, stage, d, l, publish, overwritePaths, stagedOutputs, mcdrOutputs, stagedBootstrap, mode, &counts, logger)
	return counts, resultErr
}

func applyCommitFiles(root, stage string, d *document.Manifest, l *document.Lock, publish bool, overwritePaths map[string]bool, stagedOutputs, mcdrOutputs map[string]bootstrap.Output, stagedBootstrap bool, mode installCommitMode, counts *FileChanges, logger *log.Logger) (resultErr error) {
	reconcile := mode == installCommitReconcile
	metadataOnly := mode == installCommitMetadataOnly
	receiptPath := filepath.Join(root, receipt.Filename)
	previous := receipt.New()
	raw, err := os.ReadFile(receiptPath)
	receiptExisted := err == nil
	if receiptExisted {
		parsed, err := receipt.Decode(bytes.NewReader(raw))
		if err != nil {
			return err
		}
		previous = *parsed
	} else if !os.IsNotExist(err) {
		return err
	}
	owned := map[string]receipt.Entry{}
	observedBefore := map[string]*workspacewrite.InstallImage{}
	for _, entry := range previous.Entries {
		owned[entry.Path] = entry
	}
	next := receipt.New()
	nextIndex := make(map[string]int)
	packageByDest := make(map[string]string)
	if reconcile {
		bf, err := receipt.BootstrapFingerprint(l)
		if err != nil {
			return err
		}
		pf, err := receipt.PackageFingerprint(l)
		if err != nil {
			return err
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
	sources := map[string]string{}
	sourceImages := map[string]workspacewrite.InstallImage{}
	err = func() error {
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
			hashes, err := DigestFile(p, nil)
			if err != nil {
				return err
			}
			info, err := e.Info()
			if err != nil {
				return err
			}
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
				observedBefore[relative] = &workspacewrite.InstallImage{
					SHA256: actual["sha256"],
					Mode:   workspacewrite.EffectiveInstallMode(targetInfo.Mode().Perm()),
				}
				prior, isOwned := owned[relative]
				if !isOwned {
					if overwritePaths[relative] {
						logger.Debug(
							"overwriting unowned output",
							"path", relative,
							"reason", "overwrite requested",
						)
					} else {
						return fmt.Errorf("unowned output conflict: %s", relative)
					}
				} else {
					observed := workspacewrite.InstallImage{
						SHA256: actual["sha256"],
						Mode:   workspacewrite.EffectiveInstallMode(targetInfo.Mode().Perm()),
					}
					expected := observed
					expected.SHA256 = prior.SHA256
					if prior.Mode != 0 {
						expected.Mode = workspacewrite.EffectiveInstallMode(os.FileMode(prior.Mode))
					}
					if !workspacewrite.InstallImagesEqual(&observed, &expected) {
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
				}
			case errors.Is(err, os.ErrNotExist):
				observedBefore[relative] = nil
			default:
				return err
			}
			fileMode := os.FileMode(0o644)
			if !isSeed {
				fileMode = info.Mode().Perm()
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
					SHA256:  hashes["sha256"],
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
			sourceImages[relative] = workspacewrite.InstallImage{
				SHA256: hashes["sha256"],
				Mode:   workspacewrite.EffectiveInstallMode(fileMode),
			}
			sources[relative] = p
			return nil
		})
	}()
	if err != nil {
		return err
	}
	var pruneExpected map[string]*workspacewrite.InstallImage
	if reconcile {
		pruneExpected, err = pruneTargets(
			root,
			previous.Entries,
			tracked,
			sources,
			stagedOutputs,
			stagedBootstrap,
		)
		if err != nil {
			return err
		}
	}
	receiptBytes, err := next.Snapshot()
	if err != nil {
		return err
	}
	lockPlan, err := installDocumentPlanFor(
		root,
		document.LockFilename,
		0o644,
		func() ([]byte, error) { return document.LockFile(root).Snapshot(l) },
		l.MergeBytes,
	)
	if err != nil {
		return err
	}
	var manifestPlan *installDocumentPlan
	if publish {
		manifestPlan, err = installDocumentPlanFor(
			root,
			document.ManifestFilename,
			0o644,
			func() ([]byte, error) { return document.ManifestFile(root).Snapshot(d) },
			d.MergeBytes,
		)
		if err != nil {
			return err
		}
	}
	receiptMode, err := installFileMode(receiptPath, 0o600)
	if err != nil {
		return err
	}
	receiptAfter := workspacewrite.InstallBytesImage(receiptBytes, receiptMode)
	var receiptBase *workspacewrite.InstallImage
	if receiptExisted {
		receiptBase = workspacewrite.InstallBytesImage(raw, receiptMode)
	}
	writes := make([]commitWrite, 0, len(sources)+3)
	owners := make(map[string]struct{}, len(sources)+3)
	claim := func(name string) error {
		if _, exists := owners[name]; exists {
			return fmt.Errorf("installation path %s has conflicting owners", name)
		}
		owners[name] = struct{}{}
		return nil
	}
	for name, source := range sources {
		if installPathReserved(root, name) {
			return fmt.Errorf("staged output %s is reserved", name)
		}
		if err := claim(name); err != nil {
			return err
		}
		after := sourceImages[name]
		writes = append(writes, commitWrite{
			path:   name,
			source: source,
			base:   observedBefore[name],
			after:  &after,
		})
	}
	documents := []commitWrite{
		{
			path:  document.LockFilename,
			data:  lockPlan.Data,
			base:  lockPlan.Base,
			after: &lockPlan.Image,
		},
		{
			path:  receipt.Filename,
			data:  receiptBytes,
			base:  receiptBase,
			after: receiptAfter,
		},
	}
	if publish {
		documents = append(documents, commitWrite{
			path:  document.ManifestFilename,
			data:  manifestPlan.Data,
			base:  manifestPlan.Base,
			after: &manifestPlan.Image,
		})
	}
	documentAfter := make(map[string]*workspacewrite.InstallImage, len(documents))
	baseImages := make(map[string]*workspacewrite.InstallImage, len(documents))
	for _, document := range documents {
		if err := claim(document.path); err != nil {
			return err
		}
		writes = append(writes, document)
		documentAfter[document.path] = document.after
		baseImages[document.path] = document.base
	}
	prunes := make([]commitPrune, 0, len(pruneExpected))
	for name, expected := range pruneExpected {
		if installPathMatchesReserved(root, name, document.ManifestFilename) {
			return fmt.Errorf("installation receipt path %s is reserved", name)
		}
		if err := claim(name); err != nil {
			return err
		}
		prunes = append(prunes, commitPrune{path: name, expected: expected})
	}
	verify := func(
		files *os.Root,
		entries []workspacewrite.InstallJournalEntry,
	) error {
		mutated := make(map[string]struct{}, len(entries))
		for _, entry := range entries {
			mutated[entry.Path] = struct{}{}
		}
		for name, expected := range documentAfter {
			current, err := workspacewrite.ObserveInstallImage(files, name)
			if err != nil {
				return err
			}
			compareMode := baseImages[name] != nil
			if !workspacewrite.InstallImagesMatch(current, expected, compareMode) {
				return fmt.Errorf(
					"installation document %s changed before commit",
					name,
				)
			}
		}
		for name, image := range sourceImages {
			if _, changed := mutated[name]; changed {
				continue
			}
			image := image
			current, err := workspacewrite.ObserveInstallImage(files, name)
			if err != nil {
				return err
			}
			if !workspacewrite.InstallImagesEqual(current, &image) {
				return fmt.Errorf(
					"installation path %s changed before commit",
					name,
				)
			}
		}
		for name, expected := range pruneExpected {
			if _, changed := mutated[name]; changed {
				continue
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
		}
		return nil
	}
	applied, err := executeCommit(root, commitRequest{
		label:  "installation",
		writes: writes,
		prunes: prunes,
		verify: verify,
	}, logger)
	if err != nil {
		return err
	}
	for _, entry := range applied {
		if entry.After == nil {
			if _, pruned := pruneExpected[entry.Path]; pruned {
				counts.Removed++
			}
			continue
		}
		if _, staged := sources[entry.Path]; !staged {
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
		"files", len(sources),
		"pruned", counts.Removed,
		"manifest-published", publish,
	)
	return nil
}
