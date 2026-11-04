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
	"github.com/mclucy/lucy/internal/fsutil"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/receipt"
	"gopkg.in/yaml.v3"
)

func applyCommit(root, stage string, d *document.Manifest, l *document.Lock, publish bool, overwritePaths, adoptedPaths map[string]bool, logger *log.Logger) (FileChanges, error) {
	var counts FileChanges
	resultErr := applyCommitFiles(root, stage, d, l, publish, overwritePaths, adoptedPaths, &counts, logger)
	return counts, resultErr
}

func applyCommitFiles(root, stage string, d *document.Manifest, l *document.Lock, publish bool, overwritePaths, adoptedPaths map[string]bool, counts *FileChanges, logger *log.Logger) (resultErr error) {
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
	owned := map[string]string{}
	occupantHashes := map[string]string{}
	for _, entry := range previous.Entries {
		owned[entry.Path] = entry.SHA256
	}
	next := receipt.New()
	lockFingerprint, err := receipt.ResolutionFingerprint(l)
	if err != nil {
		return err
	}
	next.LockFingerprint = lockFingerprint
	tracked := trackedPaths(l)
	sources := map[string]string{}
	sourceImages := map[string]workspacewrite.InstallImage{}
	err = filepath.WalkDir(stage, func(p string, e os.DirEntry, walkErr error) error {
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
		if strings.Contains(relative, "/.bootstrap-data/") || strings.HasPrefix(relative, ".bootstrap-data/") {
			return nil
		}
		if err := document.SafeRelativePath(relative); err != nil {
			return err
		}
		target := filepath.Join(root, relative)
		mutableDefault := filepath.Base(relative) == "config.yml" || filepath.Base(relative) == "user_jvm_args.txt"
		if tracked[strings.ToLower(relative)] {
			if !mutableDefault {
				return fmt.Errorf("tracked file %s collides with managed runtime output", relative)
			}
			return nil
		}
		if mutableDefault {
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
		if _, err := os.Lstat(target); err == nil {
			if info, err := os.Lstat(target); err != nil || info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("refuse symlink output %s", relative)
			}
			actual, err := DigestFile(target, nil)
			if err != nil {
				return err
			}
			occupantHashes[relative] = actual["sha256"]
			if actual["sha256"] != hashes["sha256"] && actual["sha256"] != owned[relative] {
				switch {
				case overwritePaths[relative]:
					logger.Debug(
						"overwriting occupied output",
						"path", relative,
						"reason", "overwrite requested",
					)
				case adoptedPaths[relative]:
					logger.Debug(
						"adopting occupied output",
						"path", relative,
						"reason", "occupied by the adopted installation",
					)
				default:
					return fmt.Errorf("unowned or modified output conflict: %s", relative)
				}
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		mode := os.FileMode(0o644)
		if !mutableDefault {
			mode = info.Mode().Perm()
			next.Entries = append(next.Entries, receipt.Entry{
				Path:   relative,
				SHA256: hashes["sha256"],
				Mode:   uint32(mode),
			})
		}
		sourceImages[relative] = workspacewrite.InstallImage{
			SHA256: hashes["sha256"],
			Mode:   effectiveInstallMode(mode),
		}
		sources[relative] = p
		return nil
	})
	if err != nil {
		return err
	}
	pruneExpected := map[string]string{}
	for _, old := range previous.Entries {
		if tracked[strings.ToLower(old.Path)] {
			continue
		}
		if _, ok := sources[old.Path]; ok {
			continue
		}
		p := filepath.Join(root, old.Path)
		if _, err := os.Stat(p); os.IsNotExist(err) {
			continue
		}
		actual, err := DigestFile(p, nil)
		if err != nil {
			return err
		}
		if actual["sha256"] != old.SHA256 {
			return fmt.Errorf("modified owned output cannot be pruned: %s", old.Path)
		}
		pruneExpected[old.Path] = old.SHA256
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
	for _, old := range previous.Entries {
		if tracked[strings.ToLower(old.Path)] {
			continue
		}
		if _, ok := sources[old.Path]; ok {
			continue
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
					entry.Before.SHA256 != expected {
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
		resultErr = errors.Join(resultErr, workspacewrite.RecoverInstall(root, logger))
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
