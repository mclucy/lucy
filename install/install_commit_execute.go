package install

import (
	"bytes"
	"crypto/rand"
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
	path     string
	data     []byte
	source   string
	base     *workspacewrite.InstallImage
	after    *workspacewrite.InstallImage
	displace bool
}

// commitPrune records one path a transaction removes. A nil expected means the
// path was already absent at planning, so that absence stays a precondition
// instead of disappearing from the journal.
type commitPrune struct {
	path     string
	expected *workspacewrite.InstallImage
}

// commitRequest describes one recoverable transaction. documents pins the
// workspace metadata a publication must not overwrite after observation;
// verify runs after every write and prune, before the commit point, and may
// refuse state the transaction would otherwise publish. Callers own their
// policy; the executor owns the journal, the rollback data, the pinned
// documents, and the recovery contract.
type commitRequest struct {
	label     string
	writes    []commitWrite
	prunes    []commitPrune
	documents WorkspaceDocuments
	verify    func(files *os.Root, entries []workspacewrite.InstallJournalEntry) error
}

// executeCommit applies a request and returns the entries it mutated, sorted by
// path. The caller must hold the workspace writer lock and must replay any
// interrupted journal first.
func executeCommit(
	root string,
	request commitRequest,
	logger *log.Logger,
) (applied []workspacewrite.InstallJournalEntry, resultErr error) {
	type change struct {
		path     string
		data     []byte
		source   string
		before   *workspacewrite.InstallImage
		after    *workspacewrite.InstallImage
		prune    bool
		displace bool
	}
	changes := make([]change, 0, len(request.writes)+len(request.prunes))
	for _, write := range request.writes {
		changes = append(changes, change{
			path:     write.path,
			data:     write.data,
			source:   write.source,
			before:   write.base,
			after:    write.after,
			displace: write.displace,
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
	if len(changes) == 0 && request.verify == nil &&
		request.documents.documents == nil {
		return nil, nil
	}
	files, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, files.Close()) }()
	if len(changes) == 0 {
		return nil, runCommitVerify(files, request, nil)
	}
	if err := request.documents.verify(files, nil); err != nil {
		return nil, err
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
	backupRelative := ".lucy/rollback-" + rand.Text()
	backup := filepath.Join(root, filepath.FromSlash(backupRelative))
	if err := files.Mkdir(filepath.FromSlash(backupRelative), 0o700); err != nil {
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
			if !change.displace && before != nil &&
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
				Path:     change.path,
				Before:   before,
				After:    change.after,
				Displace: change.displace,
			},
		)
	}
	if len(journal.Entries) == 0 {
		return nil, runCommitVerify(files, request, nil)
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
			// Stage files belong to this transaction alone, so a hardlink moves
			// one into the workspace without a copy. A stage file that is
			// itself a store link or clone keeps sharing the store content.
			// A mode change falls back to a clone or copy.
			if err := fsutil.WriteFileRoot(
				files,
				change.path,
				change.source,
				fsutil.LinkHardlink,
				fsutil.WriteOptions{
					Mode:    os.FileMode(entry.After.Mode),
					Replace: true,
					Check:   installBeforeCheck(files, entry),
				},
			); err != nil {
				return nil, err
			}
		} else if err := fsutil.WriteRoot(
			files,
			change.path,
			bytes.NewReader(change.data),
			fsutil.WriteOptions{
				Mode:    os.FileMode(entry.After.Mode),
				Replace: true,
				Check:   installBeforeCheck(files, entry),
			},
		); err != nil {
			return nil, err
		}
		applied = append(applied, entry)
	}
	if err := runCommitVerify(files, request, journal.Entries); err != nil {
		return nil, err
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
	if err := workspacewrite.CleanupCommittedInstall(files, &journal); err != nil {
		return nil, &CommittedError{Err: err}
	}
	return applied, nil
}

// runCommitVerify runs the caller's policy and then rechecks pinned documents
// at the commit point. The document check intentionally runs last: caller
// verification may hash outputs for a while, and an edit made during that work
// must still fail the transaction. It runs even when the transaction journaled
// no changes, so a publication that writes nothing still revalidates what
// callers observed.
func runCommitVerify(
	files *os.Root,
	request commitRequest,
	entries []workspacewrite.InstallJournalEntry,
) error {
	if request.verify != nil {
		if err := request.verify(files, entries); err != nil {
			return err
		}
	}
	return request.documents.verify(files, request.writes)
}

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
