package install

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mclucy/lucy/internal/fsutil"
	"github.com/mclucy/lucy/internal/workspaceguard"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/log"
	"go.yaml.in/yaml/v4"
)

// commitWrite records one path a transaction creates or replaces. Exactly one
// of data and source supplies the bytes; source names a file outside the
// workspace, such as a staged output. base is the image the planner observed,
// and after is the image the write produces. Satisfying base is the write's
// precondition, not a description of what the path currently holds.
type commitWrite struct {
	path   string
	data   []byte
	source string
	base   *workspacewrite.InstallImage
	after  *workspacewrite.InstallImage
}

// commitPrune records one path a transaction removes. A nil expected means the
// path was already absent at planning, so that absence stays a precondition
// instead of disappearing from the journal.
type commitPrune struct {
	path     string
	expected *workspacewrite.InstallImage
}

// commitRequest describes one recoverable transaction. verify runs after every
// write and prune, before the commit point, and may refuse state the
// transaction would otherwise publish. Callers own their policy; the executor
// owns the journal, the rollback data, and the recovery contract.
type commitRequest struct {
	label  string
	writes []commitWrite
	prunes []commitPrune
	verify func(files *os.Root, entries []workspacewrite.InstallJournalEntry) error
}

// executeCommit applies a request as one journaled transaction and returns the
// journal entries it mutated. A request whose paths all match their planned
// images publishes no journal. The caller must hold the workspace writer lock
// and must have replayed interrupted journals first.
func executeCommit(
	root string,
	request commitRequest,
	logger *log.Logger,
) (applied []workspacewrite.InstallJournalEntry, resultErr error) {
	type change struct {
		path   string
		data   []byte
		source string
		before *workspacewrite.InstallImage
		after  *workspacewrite.InstallImage
		prune  bool
	}
	changes := make([]change, 0, len(request.writes)+len(request.prunes))
	for _, write := range request.writes {
		changes = append(changes, change{
			path:   write.path,
			data:   write.data,
			source: write.source,
			before: write.base,
			after:  write.after,
		})
	}
	for _, prune := range request.prunes {
		changes = append(changes, change{
			path:   prune.path,
			before: prune.expected,
			prune:  true,
		})
	}
	slices.SortFunc(changes, func(left, right change) int {
		return strings.Compare(left.path, right.path)
	})
	if len(changes) == 0 {
		return nil, nil
	}
	names := make([]string, len(changes))
	for i := range changes {
		names[i] = changes[i].path
	}
	if err := rejectCommitPathAliases(root, request.label, names); err != nil {
		return nil, err
	}
	for _, name := range names {
		if installPathMatchesReserved(
			root, name, workspacewrite.InstallJournalName,
		) {
			return nil, fmt.Errorf(
				"%s path %s is reserved",
				request.label,
				name,
			)
		}
	}
	backup, err := os.MkdirTemp(filepath.Join(root, ".lucy"), "rollback-*")
	if err != nil {
		return nil, err
	}
	journalPublished := false
	defer func() {
		if journalPublished {
			return
		}
		if err := os.RemoveAll(backup); err != nil {
			logger.Warn(fmt.Errorf(
				"remove unused %s backup: %w",
				request.label,
				err,
			))
		}
	}()
	files, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, files.Close()) }()
	backupRelative := filepath.ToSlash(
		filepath.Join(".lucy", filepath.Base(backup)),
	)
	journal := workspacewrite.InstallJournal{
		Backup:  backupRelative,
		Entries: make([]workspacewrite.InstallJournalEntry, 0, len(changes)),
	}
	for _, change := range changes {
		if err := workspacewrite.SafePath(root, change.path); err != nil {
			return nil, err
		}
		before, err := workspacewrite.ObserveInstallImage(files, change.path)
		if err != nil {
			return nil, err
		}
		if change.prune {
			if change.before == nil {
				if before != nil {
					return nil, fmt.Errorf(
						"absent owned output %s reappeared before pruning",
						change.path,
					)
				}
				continue
			}
			if before == nil ||
				!workspacewrite.InstallImagesEqual(before, change.before) {
				return nil, fmt.Errorf(
					"modified or unowned output cannot be pruned: %s",
					change.path,
				)
			}
		} else {
			if !workspacewrite.InstallImagesEqual(before, change.before) {
				return nil, fmt.Errorf(
					"%s path %s changed while planning",
					request.label,
					change.path,
				)
			}
			if before != nil &&
				workspacewrite.InstallImagesEqual(before, change.after) {
				continue
			}
		}
		if before != nil {
			if err := preserveInstallPreImage(
				files,
				backup,
				backupRelative,
				*before,
				filepath.Join(root, change.path),
			); err != nil {
				return nil, err
			}
		}
		journal.Entries = append(
			journal.Entries,
			workspacewrite.InstallJournalEntry{
				Path:   change.path,
				Before: before,
				After:  change.after,
			},
		)
	}
	if len(journal.Entries) == 0 {
		return nil, nil
	}
	journalBytes, err := yaml.Marshal(journal)
	if err != nil {
		return nil, err
	}
	journalPath := filepath.Join(root, workspacewrite.InstallJournalName)
	if err := fsutil.AtomicWrite(journalPath, journalBytes, 0o600); err != nil {
		// AtomicWrite can return a sync error after the journal replacement was
		// published. Keep the backup while any journal remains, so recovery can
		// still verify and remove it.
		if _, statErr := os.Lstat(journalPath); statErr == nil {
			journalPublished = true
		}
		return nil, err
	}
	journalPublished = true
	logger.Debug(
		request.label+" transaction journaled",
		"journal", journalPath,
		"backup", journal.Backup,
		"paths", len(journal.Entries),
	)
	mutations := make(
		map[string]workspacewrite.InstallJournalEntry,
		len(journal.Entries),
	)
	for _, entry := range journal.Entries {
		mutations[entry.Path] = entry
	}
	failed := true
	defer func() {
		if !failed {
			return
		}
		committed, _ := workspacewrite.InstallJournalCommitted(root)
		resultErr = errors.Join(
			resultErr,
			workspaceguard.RecoverInstall(root, logger),
		)
		if committed && resultErr != nil {
			resultErr = &CommittedError{Err: fmt.Errorf(
				"%s committed; %w",
				request.label,
				resultErr,
			)}
		}
	}()
	for _, change := range changes {
		entry, mutated := mutations[change.path]
		if !mutated {
			continue
		}
		if change.prune {
			current, err := workspacewrite.ObserveInstallImage(
				files, change.path,
			)
			if err != nil {
				return nil, err
			}
			if !workspacewrite.InstallImagesEqual(current, entry.Before) {
				return nil, fmt.Errorf(
					"%s output %s changed before pruning",
					request.label,
					change.path,
				)
			}
			if err := files.Remove(change.path); err != nil {
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				return nil, err
			}
			applied = append(applied, entry)
			continue
		}
		parent := filepath.Dir(change.path)
		if parent != "." {
			if err := files.MkdirAll(parent, 0o755); err != nil {
				return nil, err
			}
		}
		if change.source != "" {
			input, err := os.Open(change.source)
			if err != nil {
				return nil, err
			}
			copyErr := fsutil.WriteRoot(
				files,
				change.path,
				input,
				fsutil.WriteOptions{
					Mode:    os.FileMode(entry.After.Mode),
					Replace: true,
					Check:   installBeforeCheck(files, entry),
				},
			)
			closeErr := input.Close()
			if copyErr != nil || closeErr != nil {
				return nil, errors.Join(copyErr, closeErr)
			}
		} else if err := publishInstallBytes(
			files,
			change.path,
			change.data,
			entry.After,
			entry,
		); err != nil {
			return nil, err
		}
		applied = append(applied, entry)
	}
	if request.verify != nil {
		if err := request.verify(files, journal.Entries); err != nil {
			return nil, err
		}
	}
	journal.Committed = true
	committedBytes, err := yaml.Marshal(journal)
	if err != nil {
		return nil, err
	}
	if err := fsutil.AtomicWrite(journalPath, committedBytes, 0o600); err != nil {
		return nil, err
	}
	failed = false
	if err := os.RemoveAll(backup); err != nil {
		return nil, &CommittedError{Err: fmt.Errorf(
			"%s committed; remove rollback backup: %w",
			request.label,
			err,
		)}
	}
	if err := workspacewrite.RemoveRoot(
		files, workspacewrite.InstallJournalName,
	); err != nil {
		return nil, &CommittedError{Err: fmt.Errorf(
			"%s committed; remove transaction journal: %w",
			request.label,
			err,
		)}
	}
	return applied, nil
}

func publishInstallBytes(
	files *os.Root,
	name string,
	data []byte,
	image *workspacewrite.InstallImage,
	entry workspacewrite.InstallJournalEntry,
) error {
	return fsutil.WriteRoot(
		files,
		name,
		bytes.NewReader(data),
		fsutil.WriteOptions{
			Mode:    os.FileMode(image.Mode),
			Replace: true,
			Check:   installBeforeCheck(files, entry),
		},
	)
}

// installBeforeCheck guards a publication against an edit that happened after
// the journal captured the before image.
func installBeforeCheck(
	files *os.Root,
	entry workspacewrite.InstallJournalEntry,
) func() error {
	return func() error {
		current, err := workspacewrite.ObserveInstallImage(files, entry.Path)
		if err != nil {
			return err
		}
		if !workspacewrite.InstallImagesEqual(current, entry.Before) {
			return fmt.Errorf(
				"installation output %s changed before publication",
				entry.Path,
			)
		}
		return nil
	}
}

// preserveInstallPreImage stores one complete content-addressed before image
// through a synced root-confined publish and verifies the copy. A failure here
// happens before the journal publishes, so the caller removes the unused
// backup directory.
func preserveInstallPreImage(
	files *os.Root,
	backupPath string,
	backupRelative string,
	image workspacewrite.InstallImage,
	source string,
) error {
	relative := filepath.Join(filepath.FromSlash(backupRelative), image.SHA256)
	if info, err := files.Lstat(relative); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf(
				"installation pre-image %s is not a regular file",
				image.SHA256,
			)
		}
	} else if errors.Is(err, os.ErrNotExist) {
		input, err := os.Open(source)
		if err != nil {
			return err
		}
		copyErr := fsutil.WriteRoot(
			files,
			relative,
			input,
			fsutil.WriteOptions{Mode: 0o600, Replace: true},
		)
		closeErr := input.Close()
		if copyErr != nil || closeErr != nil {
			return errors.Join(copyErr, closeErr)
		}
	} else {
		return err
	}
	hashes, err := DigestFile(filepath.Join(backupPath, image.SHA256), nil)
	if err != nil {
		return err
	}
	if hashes["sha256"] != image.SHA256 {
		return fmt.Errorf("installation pre-image %s is incomplete", image.SHA256)
	}
	return nil
}
