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
	"gopkg.in/yaml.v3"
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
	occupantHashes := map[string]string{}
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
			dest, err := packageDestination(l, p)
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
				occupantHashes[relative] = actual["sha256"]
				observedBefore[relative] = &workspacewrite.InstallImage{
					SHA256: actual["sha256"],
					Mode:   effectiveInstallMode(targetInfo.Mode().Perm()),
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
						Mode:   effectiveInstallMode(targetInfo.Mode().Perm()),
					}
					expected := observed
					expected.SHA256 = prior.SHA256
					if prior.Mode != 0 {
						expected.Mode = effectiveInstallMode(os.FileMode(prior.Mode))
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
				Mode:   effectiveInstallMode(fileMode),
			}
			sources[relative] = p
			return nil
		})
	}()
	if err != nil {
		return err
	}
	pruneExpected := map[string]workspacewrite.InstallImage{}
	if reconcile {
		for _, old := range previous.Entries {
			if tracked[strings.ToLower(old.Path)] {
				continue
			}
			if _, ok := sources[old.Path]; ok {
				continue
			}
			if out, classified := stagedOutputs[old.Path]; classified &&
				stagedOutputPreserved(out) {
				continue
			}
			switch old.Origin {
			case receipt.OriginBootstrap:
				if !stagedBootstrap {
					continue
				}
			case receipt.OriginPackage, receipt.OriginMCDR:
			default:
				return fmt.Errorf("unrecognized entry origin %q for path %s; refusing implicit prune", old.Origin, old.Path)
			}
			p := filepath.Join(root, old.Path)
			info, err := os.Lstat(p)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return err
			}
			if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
				return fmt.Errorf("owned output is not a regular file: %s", old.Path)
			}
			actual, err := DigestFile(p, nil)
			if err != nil {
				return err
			}
			observed := workspacewrite.InstallImage{
				SHA256: actual["sha256"],
				Mode:   effectiveInstallMode(info.Mode().Perm()),
			}
			if !strings.EqualFold(observed.SHA256, old.SHA256) {
				return fmt.Errorf("modified owned output cannot be pruned: %s", old.Path)
			}
			if old.Mode != 0 &&
				effectiveInstallMode(os.FileMode(old.Mode)) != observed.Mode {
				return fmt.Errorf("modified owned output cannot be pruned: %s", old.Path)
			}
			// Record the mode observed at planning time so a later chmod is
			// still caught before the journal captures its before image.
			pruneExpected[old.Path] = observed
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
	receiptAfter := installBytesImage(receiptBytes, receiptMode)
	var receiptBase *workspacewrite.InstallImage
	if receiptExisted {
		receiptBase = installBytesImage(raw, receiptMode)
	}
	afterImages := make(map[string]*workspacewrite.InstallImage, len(sources)+3)
	for name, image := range sourceImages {
		if installPathReserved(root, name) {
			return fmt.Errorf("staged output %s is reserved", name)
		}
		image := image
		afterImages[name] = &image
	}
	planAfter := func(name string, image *workspacewrite.InstallImage) error {
		if installPathMatchesReserved(
			root, name, workspacewrite.InstallJournalName,
		) {
			return fmt.Errorf("installation path %s is reserved", name)
		}
		if _, exists := afterImages[name]; exists {
			return fmt.Errorf("installation path %s has conflicting owners", name)
		}
		afterImages[name] = image
		return nil
	}
	if err := planAfter(document.LockFilename, &lockPlan.Image); err != nil {
		return err
	}
	if publish {
		if err := planAfter(document.ManifestFilename, &manifestPlan.Image); err != nil {
			return err
		}
	}
	if err := planAfter(receipt.Filename, receiptAfter); err != nil {
		return err
	}
	baseImages := map[string]*workspacewrite.InstallImage{
		document.LockFilename: lockPlan.Base,
		receipt.Filename:      receiptBase,
	}
	documentAfter := map[string]*workspacewrite.InstallImage{
		document.LockFilename: &lockPlan.Image,
		receipt.Filename:      receiptAfter,
	}
	if publish {
		baseImages[document.ManifestFilename] = manifestPlan.Base
		documentAfter[document.ManifestFilename] = &manifestPlan.Image
	}
	if reconcile {
		for _, old := range previous.Entries {
			if tracked[strings.ToLower(old.Path)] {
				continue
			}
			if _, ok := sources[old.Path]; ok {
				continue
			}
			if out, classified := stagedOutputs[old.Path]; classified &&
				stagedOutputPreserved(out) {
				continue
			}
			switch old.Origin {
			case receipt.OriginBootstrap:
				if !stagedBootstrap {
					continue
				}
			case receipt.OriginPackage, receipt.OriginMCDR:
			default:
				return fmt.Errorf("unrecognized entry origin %q for path %s; refusing implicit prune", old.Origin, old.Path)
			}
			if _, err := os.Stat(filepath.Join(root, old.Path)); err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return err
			}
			if installPathMatchesReserved(
				root, old.Path, document.ManifestFilename,
			) {
				return fmt.Errorf("installation receipt path %s is reserved", old.Path)
			}
			if err := planAfter(old.Path, nil); err != nil {
				return err
			}
		}
	}
	names := make([]string, 0, len(afterImages))
	for name := range afterImages {
		names = append(names, name)
	}
	slices.Sort(names)
	if err := rejectInstallPathAliases(root, names); err != nil {
		return err
	}
	backup, err := os.MkdirTemp(filepath.Join(root, ".lucy"), "rollback-*")
	if err != nil {
		return err
	}
	journalPublished := false
	defer func() {
		if journalPublished {
			return
		}
		if err := os.RemoveAll(backup); err != nil {
			logger.Warn(fmt.Errorf("remove unused installation backup: %w", err))
		}
	}()
	backupRelative := filepath.ToSlash(
		filepath.Join(".lucy", filepath.Base(backup)),
	)
	journal := workspacewrite.InstallJournal{
		Backup:  backupRelative,
		Entries: make([]workspacewrite.InstallJournalEntry, 0, len(names)),
	}
	prepareErr := func() (resultErr error) {
		files, err := os.OpenRoot(root)
		if err != nil {
			return err
		}
		defer func() { resultErr = errors.Join(resultErr, files.Close()) }()
		for _, name := range names {
			if err := workspacewrite.SafePath(root, name); err != nil {
				return err
			}
			target := filepath.Join(root, name)
			entry := workspacewrite.InstallJournalEntry{
				Path:  name,
				After: afterImages[name],
			}
			info, err := files.Lstat(name)
			switch {
			case err == nil:
				if !info.Mode().IsRegular() {
					return fmt.Errorf("installation target %s is not a regular file", name)
				}
				hashes, err := DigestFile(target, nil)
				if err != nil {
					return err
				}
				before := workspacewrite.InstallImage{
					SHA256: hashes["sha256"],
					Mode:   effectiveInstallMode(info.Mode().Perm()),
				}
				entry.Before = &before
			case errors.Is(err, os.ErrNotExist):
			default:
				return err
			}
			if expected, observed := observedBefore[name]; observed {
				if !workspacewrite.InstallImagesEqual(entry.Before, expected) {
					return fmt.Errorf(
						"installation path %s changed while planning",
						name,
					)
				}
			}
			if base, planned := baseImages[name]; planned {
				if !workspacewrite.InstallImagesEqual(entry.Before, base) {
					return fmt.Errorf(
						"installation path %s changed while planning",
						name,
					)
				}
			}
			if entry.Before == nil && entry.After == nil {
				continue
			}
			if entry.After == nil {
				expected, validated := pruneExpected[name]
				if !validated || entry.Before == nil ||
					!workspacewrite.InstallImagesEqual(entry.Before, &expected) {
					return fmt.Errorf(
						"modified or unowned output cannot be pruned: %s",
						name,
					)
				}
			}
			if entry.Before != nil && entry.After != nil &&
				workspacewrite.InstallImagesEqual(entry.Before, entry.After) {
				continue
			}
			if entry.Before != nil {
				if err := preserveInstallPreImage(
					files, backup, backupRelative, *entry.Before, target,
				); err != nil {
					return err
				}
			}
			journal.Entries = append(journal.Entries, entry)
		}
		return nil
	}()
	if prepareErr != nil {
		return prepareErr
	}
	journalBytes, err := yaml.Marshal(journal)
	if err != nil {
		return err
	}
	journalPath := filepath.Join(root, workspacewrite.InstallJournalName)
	if err := fsutil.AtomicWrite(journalPath, journalBytes, 0o600); err != nil {
		// AtomicWrite can return a sync error after the journal replacement was
		// published. Keep the backup while any journal remains, so recovery can
		// still verify and remove it.
		if _, statErr := os.Lstat(journalPath); statErr == nil {
			journalPublished = true
		}
		return err
	}
	journalPublished = true
	logger.Debug(
		"installation transaction journaled",
		"journal", journalPath,
		"backup", journal.Backup,
		"paths", len(journal.Entries),
	)
	mutations := make(map[string]workspacewrite.InstallJournalEntry, len(journal.Entries))
	for _, entry := range journal.Entries {
		mutations[entry.Path] = entry
	}
	failed := true
	defer func() {
		if !failed {
			return
		}
		committed, _ := workspacewrite.InstallJournalCommitted(root)
		resultErr = errors.Join(resultErr, workspaceguard.RecoverInstall(root, logger))
		if committed && resultErr != nil {
			resultErr = &CommittedError{Err: fmt.Errorf(
				"installation committed; %w", resultErr,
			)}
		}
	}()
	applyErr := func() (resultErr error) {
		files, err := os.OpenRoot(root)
		if err != nil {
			return err
		}
		defer func() { resultErr = errors.Join(resultErr, files.Close()) }()
		for name, source := range sources {
			entry, mutated := mutations[name]
			if !mutated {
				continue
			}
			image := sourceImages[name]
			_, statErr := files.Lstat(name)
			exists := statErr == nil
			if !exists {
				counts.Created++
			} else if occupantHashes[name] != image.SHA256 {
				counts.Replaced++
			}
			parent := filepath.Dir(name)
			if parent != "." {
				if err := files.MkdirAll(parent, 0o755); err != nil {
					return err
				}
			}
			input, err := os.Open(source)
			if err != nil {
				return err
			}
			copyErr := fsutil.WriteRoot(
				files,
				name,
				input,
				fsutil.WriteOptions{
					Mode:    os.FileMode(entry.After.Mode),
					Replace: true,
					Check:   installBeforeCheck(files, entry),
				},
			)
			closeErr := input.Close()
			if copyErr != nil || closeErr != nil {
				return errors.Join(copyErr, closeErr)
			}
		}
		pruned := 0
		if reconcile {
			for _, old := range previous.Entries {
				if tracked[strings.ToLower(old.Path)] {
					continue
				}
				if _, ok := sources[old.Path]; ok {
					continue
				}
				entry, mutated := mutations[old.Path]
				if !mutated {
					continue
				}
				current, err := observeInstallPath(files, old.Path)
				if err != nil {
					return err
				}
				if !workspacewrite.InstallImagesEqual(current, entry.Before) {
					return fmt.Errorf(
						"installation output %s changed before pruning",
						old.Path,
					)
				}
				if err := files.Remove(old.Path); err != nil {
					if errors.Is(err, os.ErrNotExist) {
						continue
					}
					return err
				}
				pruned++
			}
		}
		counts.Removed = pruned
		if publish {
			if entry, mutated := mutations[document.ManifestFilename]; mutated {
				if err := publishInstallBytes(
					files,
					document.ManifestFilename,
					manifestPlan.Data,
					&manifestPlan.Image,
					entry,
				); err != nil {
					return err
				}
			}
		}
		if entry, mutated := mutations[document.LockFilename]; mutated {
			if err := publishInstallBytes(
				files,
				document.LockFilename,
				lockPlan.Data,
				&lockPlan.Image,
				entry,
			); err != nil {
				return err
			}
		}
		if entry, mutated := mutations[receipt.Filename]; mutated {
			if err := publishInstallBytes(
				files,
				receipt.Filename,
				receiptBytes,
				receiptAfter,
				entry,
			); err != nil {
				return err
			}
		}
		for name, expected := range documentAfter {
			current, err := observeInstallPath(files, name)
			if err != nil {
				return err
			}
			compareMode := baseImages[name] != nil
			if !installFinalImageMatches(current, expected, compareMode) {
				return fmt.Errorf(
					"installation document %s changed before commit",
					name,
				)
			}
		}
		for name, expected := range afterImages {
			if _, mutated := mutations[name]; mutated {
				continue
			}
			if _, document := documentAfter[name]; document {
				continue
			}
			current, err := observeInstallPath(files, name)
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
	}()
	if applyErr != nil {
		return applyErr
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
			"installation committed; remove rollback backup: %w", err,
		)}
	}
	if err := os.Remove(journalPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return &CommittedError{Err: fmt.Errorf(
			"installation committed; remove transaction journal: %w", err,
		)}
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
