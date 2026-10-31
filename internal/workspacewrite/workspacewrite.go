// Package workspacewrite coordinates recoverable filesystem writes without runtime ownership policy.
package workspacewrite

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/internal/fsutil"
	"github.com/mclucy/lucy/log"
	"gopkg.in/yaml.v3"
)

const (
	InstallJournalName = ".lucy/commit.yaml"

	// LockFileName names the writer lock inside the workspace state directory.
	LockFileName = "writer.lock"
)

// InstallImage describes one content state of a path. A nil image means the
// path is absent.
type InstallImage struct {
	SHA256 string `yaml:"sha256"`
	Mode   uint32 `yaml:"mode"`
}

// InstallJournalEntry records the before and after content and permission
// state of one path that a commit may touch. A nil Before marks a path the
// commit creates; a nil After marks a path the commit prunes. Content hashes
// and modes identify whether recovery may reverse the transition; Mode also
// supplies the permissions to restore.
type InstallJournalEntry struct {
	Path   string        `yaml:"path"`
	Before *InstallImage `yaml:"before,omitempty"`
	After  *InstallImage `yaml:"after,omitempty"`
}

// InstallJournal is the installation rollback record. Backup is relative to
// the workspace root, so moving the workspace keeps it valid. Committed marks
// the commit point: once set, recovery only removes rollback data and never
// rewrites workspace files. Decoding is strict, so a legacy changed/created
// journal or any unknown field is refused instead of reinterpreted.
type InstallJournal struct {
	Backup    string                `yaml:"backup"`
	Committed bool                  `yaml:"committed,omitempty"`
	Entries   []InstallJournalEntry `yaml:"entries"`
}

// RecoveryCleanupError reports that recovery of a committed installation could
// not remove its rollback data. Installed files and documents stay in place.
type RecoveryCleanupError struct{ Err error }

func (e *RecoveryCleanupError) Error() string { return e.Err.Error() }

func (e *RecoveryCleanupError) Unwrap() error { return e.Err }

// Lock is shared by installation and explicit file operations. It reclaims
// staging that a killed publisher abandoned, which is safe only under the
// writer lock it takes, because that lock proves no publisher is live.
func Lock(root string, logger *log.Logger) (*fsutil.FileLock, error) {
	if err := SafePath(root, ".lucy"); err != nil {
		return nil, err
	}
	if err := fsutil.MkdirAllPrivate(filepath.Join(root, ".lucy")); err != nil {
		return nil, err
	}
	if err := SafePath(root, ".lucy/"+LockFileName); err != nil {
		return nil, err
	}
	lock, err := fsutil.TryLockFile(filepath.Join(root, ".lucy", LockFileName))
	if err != nil {
		if errors.Is(err, fsutil.ErrLocked) {
			return nil, fmt.Errorf("another Lucy operation owns this workspace: %w", err)
		}
		return nil, err
	}
	files, err := os.OpenRoot(root)
	if err != nil {
		return nil, errors.Join(err, lock.Close())
	}
	removed, sweepErr := fsutil.SweepStaging(files)
	closeErr := files.Close()
	if sweepErr != nil {
		logger.Warn(fmt.Errorf("reclaim stale staging files: %w", sweepErr))
	}
	for _, name := range removed {
		logger.Debug("removed stale staging file", "path", name)
	}
	if closeErr != nil {
		return nil, errors.Join(closeErr, lock.Close())
	}
	return lock, nil
}

// SafePath rejects existing symlink components before root-confined IO.
func SafePath(root, relative string) error {
	if !filepath.IsLocal(relative) {
		return fmt.Errorf("output path must stay inside the workspace")
	}
	current := root
	for component := range strings.SplitSeq(filepath.Clean(relative), string(filepath.Separator)) {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("output path traverses symlink: %s", relative)
		}
	}
	return nil
}

// RemoveRoot persists disappearance from its original name; leftovers are private cleanup data.
func RemoveRoot(root *os.Root, relative string) error {
	if !filepath.IsLocal(relative) {
		return fmt.Errorf("output path must stay inside the workspace")
	}
	if _, err := root.Lstat(relative); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if err := root.MkdirAll(".lucy/tmp", 0o700); err != nil {
		return err
	}
	garbage := ".lucy/tmp/removed-" + rand.Text()
	if err := fsutil.RenameRoot(root, relative, garbage); err != nil {
		return err
	}
	if err := root.Chmod(garbage, 0o600); err != nil {
		return err
	}
	return root.Remove(garbage)
}

// RecoverInstall must run with workspace ownership before reading state for a new mutation.
func RecoverInstall(root string, logger *log.Logger) (resultErr error) {
	journal, err := readInstallJournal(root)
	if err != nil {
		return err
	}
	if journal == nil {
		return nil
	}
	if err := CheckInactive(root, "recovering an interrupted installation"); err != nil {
		return err
	}
	backup, err := installJournalBackup(root, journal.Backup)
	if err != nil {
		return err
	}
	journalPath := filepath.Join(root, InstallJournalName)
	if journal.Committed {
		logger.Warn(
			fmt.Errorf(
				"completing cleanup of committed installation transaction %s",
				journalPath,
			),
		)
		if err := cleanupCommittedInstall(root, backup, journalPath, logger); err != nil {
			return err
		}
		return nil
	}
	if err := validateInstallJournal(root, journal); err != nil {
		return err
	}
	logger.Warn(
		fmt.Errorf(
			"recovering interrupted installation transaction %s: %d recorded paths",
			journalPath, len(journal.Entries),
		),
	)
	files, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, files.Close()) }()
	plan := make([]InstallJournalEntry, 0, len(journal.Entries))
	refused := make([]string, 0)
	for _, entry := range journal.Entries {
		current, err := observeInstallImage(files, filepath.FromSlash(entry.Path))
		if err != nil {
			return err
		}
		switch classifyInstallEntry(current, entry.Before, entry.After) {
		case installEntryUnapplied:
			continue
		case installEntryApplied:
			if err := checkInstallPreImage(files, backup, entry); err != nil {
				return err
			}
			plan = append(plan, entry)
		default:
			refused = append(refused, entry.Path)
		}
	}
	if len(refused) > 0 {
		return fmt.Errorf(
			"installation recovery refused paths edited after the interruption in %s; keep the journal and resolve these edits: %s",
			journalPath, strings.Join(refused, ", "),
		)
	}
	for _, entry := range plan {
		if err := restoreInstallEntry(files, backup, entry); err != nil {
			return err
		}
	}
	if err := files.RemoveAll(backup); err != nil {
		return err
	}
	if err := RemoveRoot(files, InstallJournalName); err != nil {
		return err
	}
	logger.Info(
		"installation transaction recovered",
		"root", root,
		"journal", journalPath,
	)
	return nil
}

// InstallJournalCommitted reports whether a pending installation journal
// reached its commit point. It is a read-only query for a committing caller
// whose committed-marker publication reported an error after the atomic
// replacement may already have happened.
func InstallJournalCommitted(root string) (bool, error) {
	journal, err := readInstallJournal(root)
	if err != nil {
		return false, err
	}
	return journal != nil && journal.Committed, nil
}

func readInstallJournal(root string) (*InstallJournal, error) {
	if err := SafePath(root, InstallJournalName); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(filepath.Join(root, InstallJournalName))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var journal InstallJournal
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&journal); err != nil {
		return nil, fmt.Errorf("decode installation journal: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("installation journal must contain one YAML document")
		}
		return nil, fmt.Errorf("decode installation journal tail: %w", err)
	}
	return &journal, nil
}

// installJournalBackup returns the workspace-relative rollback directory.
// Journals never carry an absolute path or an alias outside .lucy.
func installJournalBackup(root, backup string) (string, error) {
	relative := filepath.Clean(filepath.FromSlash(backup))
	if !filepath.IsLocal(relative) ||
		filepath.ToSlash(relative) != backup ||
		filepath.Dir(relative) != ".lucy" ||
		!strings.HasPrefix(filepath.Base(relative), "rollback-") {
		return "", fmt.Errorf("invalid installation rollback directory")
	}
	if err := SafePath(root, relative); err != nil {
		return "", err
	}
	return relative, nil
}

func validateInstallJournal(root string, journal *InstallJournal) error {
	folded := make(map[string][]string, len(journal.Entries))
	for _, entry := range journal.Entries {
		if entry.Path == "" || !filepath.IsLocal(filepath.FromSlash(entry.Path)) {
			return fmt.Errorf("invalid installation journal path %q", entry.Path)
		}
		if filepath.ToSlash(filepath.Clean(filepath.FromSlash(entry.Path))) != entry.Path {
			return fmt.Errorf("installation journal path %q is not canonical", entry.Path)
		}
		if entry.Path == InstallJournalName ||
			(strings.EqualFold(entry.Path, InstallJournalName) &&
				InstallPathsAlias(root, InstallJournalName, entry.Path)) {
			return fmt.Errorf("installation journal cannot record itself")
		}
		key := strings.ToLower(entry.Path)
		for _, prior := range folded[key] {
			if InstallPathsAlias(root, prior, entry.Path) {
				return fmt.Errorf(
					"installation journal paths %q and %q alias the same filesystem entry",
					prior, entry.Path,
				)
			}
		}
		folded[key] = append(folded[key], entry.Path)
		if entry.Before == nil && entry.After == nil {
			return fmt.Errorf(
				"installation journal path %q has neither before nor after image",
				entry.Path,
			)
		}
		for _, image := range []*InstallImage{entry.Before, entry.After} {
			if image == nil {
				continue
			}
			if err := validateInstallImage(image); err != nil {
				return fmt.Errorf(
					"invalid installation journal image for %s: %w",
					entry.Path, err,
				)
			}
		}
		if err := SafePath(root, filepath.FromSlash(entry.Path)); err != nil {
			return err
		}
	}
	return nil
}

func validateInstallImage(image *InstallImage) error {
	if len(image.SHA256) != sha256.Size*2 {
		return fmt.Errorf("sha256 must be %d hex characters", sha256.Size*2)
	}
	if _, err := hex.DecodeString(image.SHA256); err != nil {
		return fmt.Errorf("sha256 is not hex: %w", err)
	}
	if image.Mode&^uint32(0o777) != 0 {
		return fmt.Errorf("mode %#o has non-permission bits", image.Mode)
	}
	return nil
}

func cleanupCommittedInstall(root, backup, journalPath string, logger *log.Logger) (resultErr error) {
	files, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, files.Close()) }()
	if err := files.RemoveAll(backup); err != nil {
		return &RecoveryCleanupError{Err: fmt.Errorf(
			"installation committed; remove rollback backup %s: %w", backup, err,
		)}
	}
	if err := RemoveRoot(files, InstallJournalName); err != nil {
		return &RecoveryCleanupError{Err: fmt.Errorf(
			"installation committed; remove transaction journal: %w", err,
		)}
	}
	logger.Info(
		"installation transaction cleanup completed",
		"root", root,
		"journal", journalPath,
	)
	return nil
}

// installEntryState describes how far a commit got with one journal path.
type installEntryState int

const (
	// installEntryUnapplied means the path still holds its before image, so
	// recovery leaves it alone.
	installEntryUnapplied installEntryState = iota
	// installEntryApplied means the path holds its after image, so recovery
	// reverses the transition using the complete before image.
	installEntryApplied
	// installEntryModified means the path holds neither image, so something
	// edited it after the interruption. Recovery must not destroy that edit.
	installEntryModified
)

func classifyInstallEntry(current, before, after *InstallImage) installEntryState {
	// Modes distinguish states only when content is equal. If content differs,
	// some filesystems cannot represent the requested mode, so requiring a mode
	// match would misclassify their own committed bytes as an external edit.
	compareMode := before != nil && after != nil &&
		before.SHA256 == after.SHA256
	if installImagesMatch(current, before, compareMode) {
		return installEntryUnapplied
	}
	if installImagesMatch(current, after, compareMode) {
		return installEntryApplied
	}
	return installEntryModified
}

func installImagesMatch(
	current, expected *InstallImage,
	compareMode bool,
) bool {
	if current == nil || expected == nil {
		return current == nil && expected == nil
	}
	if current.SHA256 != expected.SHA256 {
		return false
	}
	return !compareMode || current.Mode == expected.Mode
}

// InstallPathsAlias reports whether two workspace-relative paths would resolve
// to the same filesystem entry. Existing shared entries are detected directly.
// Otherwise each case-differing component is checked against the filesystem of
// its parent directory, so a nested mount's semantics win over the workspace
// root's. An unprovable parent is treated as aliasing so callers refuse rather
// than risk data loss.
func InstallPathsAlias(root, left, right string) bool {
	leftInfo, leftErr := os.Lstat(filepath.Join(root, filepath.FromSlash(left)))
	rightInfo, rightErr := os.Lstat(filepath.Join(root, filepath.FromSlash(right)))
	if leftErr == nil && rightErr == nil && os.SameFile(leftInfo, rightInfo) {
		return true
	}
	leftParts := strings.Split(filepath.ToSlash(left), "/")
	rightParts := strings.Split(filepath.ToSlash(right), "/")
	if len(leftParts) != len(rightParts) {
		return false
	}
	prefix := ""
	for i := range leftParts {
		leftPart, rightPart := leftParts[i], rightParts[i]
		if leftPart == rightPart {
			prefix = nextInstallPrefix(prefix, leftPart)
			continue
		}
		if !strings.EqualFold(leftPart, rightPart) {
			return false
		}
		folded, known := installPrefixFoldsCase(root, prefix)
		if !known {
			return true
		}
		if !folded {
			return false
		}
		prefix = nextInstallPrefix(prefix, leftPart)
	}
	return true
}

func nextInstallPrefix(prefix, component string) string {
	if prefix == "" {
		return component
	}
	return prefix + "/" + component
}

func installPrefixFoldsCase(root, prefix string) (folded, known bool) {
	if prefix == "" {
		return installRootFoldsCase(root)
	}
	return installPathFoldsCase(
		root,
		filepath.Join(filepath.FromSlash(prefix), "case-probe"),
	)
}

// installPathFoldsCase reports whether the nearest existing parent directory
// of relative treats case variants as the same name. known is false when the
// directory has no usable entry to prove either behavior.
func installPathFoldsCase(root, relative string) (folded, known bool) {
	dir := filepath.Dir(filepath.FromSlash(relative))
	for {
		if dir == "." || dir == string(filepath.Separator) {
			return installRootFoldsCase(root)
		}
		absolute := filepath.Join(root, dir)
		info, err := os.Lstat(absolute)
		switch {
		case err == nil:
			if !info.IsDir() {
				return true, false
			}
			return installDirectoryFoldsCase(absolute)
		case errors.Is(err, os.ErrNotExist):
			parent := filepath.Dir(dir)
			if parent == dir {
				return installRootFoldsCase(root)
			}
			dir = parent
		default:
			return true, false
		}
	}
}

// installRootFoldsCase probes the workspace state directory, which the writer
// lock guarantees exists. A case variant of .lucy resolving to the same
// directory proves a case-insensitive root filesystem.
func installRootFoldsCase(root string) (folded, known bool) {
	lower, err := os.Lstat(filepath.Join(root, ".lucy"))
	if err != nil {
		return true, false
	}
	upper, err := os.Lstat(filepath.Join(root, ".LUCY"))
	switch {
	case err == nil:
		return os.SameFile(lower, upper), true
	case errors.Is(err, os.ErrNotExist):
		return false, true
	default:
		return true, false
	}
}

// installDirectoryFoldsCase probes one existing directory with one of its own
// entries. An entry whose case variant resolves to the same file proves a
// case-insensitive directory filesystem; a missing or distinct variant proves
// a case-sensitive one.
func installDirectoryFoldsCase(directory string) (folded, known bool) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return true, false
	}
	for _, entry := range entries {
		variant := flipNameCase(entry.Name())
		if variant == "" || variant == entry.Name() {
			continue
		}
		original, err := entry.Info()
		if err != nil {
			continue
		}
		probe, err := os.Lstat(filepath.Join(directory, variant))
		switch {
		case err == nil:
			return os.SameFile(original, probe), true
		case errors.Is(err, os.ErrNotExist):
			return false, true
		}
	}
	return true, false
}

// flipNameCase changes the case of the first ASCII letter in name.
func flipNameCase(name string) string {
	for i := range len(name) {
		switch {
		case name[i] >= 'a' && name[i] <= 'z':
			return name[:i] + string(name[i]-'a'+'A') + name[i+1:]
		case name[i] >= 'A' && name[i] <= 'Z':
			return name[:i] + string(name[i]-'A'+'a') + name[i+1:]
		}
	}
	return ""
}

// InstallImagesEqual reports whether two recorded states match exactly,
// including permission mode. Commit planning uses it to decide whether a
// publication would change anything.
func InstallImagesEqual(left, right *InstallImage) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.SHA256 == right.SHA256 && left.Mode == right.Mode
}

func observeInstallImage(files *os.Root, relative string) (*InstallImage, error) {
	info, err := files.Lstat(relative)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf(
			"installation journal path %s is not a regular file",
			relative,
		)
	}
	file, err := files.Open(relative)
	if err != nil {
		return nil, err
	}
	hasher := sha256.New()
	_, copyErr := io.Copy(hasher, file)
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		return nil, errors.Join(copyErr, closeErr)
	}
	return &InstallImage{
		SHA256: hex.EncodeToString(hasher.Sum(nil)),
		Mode:   uint32(info.Mode().Perm()),
	}, nil
}

func checkInstallPreImage(
	files *os.Root,
	backup string,
	entry InstallJournalEntry,
) error {
	if entry.Before == nil {
		return nil
	}
	info, err := files.Lstat(filepath.Join(backup, entry.Before.SHA256))
	if err != nil {
		return fmt.Errorf(
			"installation pre-image %s for %s is unavailable: %w",
			entry.Before.SHA256, entry.Path, err,
		)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf(
			"installation pre-image %s for %s is not a regular file",
			entry.Before.SHA256, entry.Path,
		)
	}
	return nil
}

func restoreInstallEntry(
	files *os.Root,
	backup string,
	entry InstallJournalEntry,
) (resultErr error) {
	relative := filepath.FromSlash(entry.Path)
	if entry.Before == nil {
		current, err := observeInstallImage(files, relative)
		if err != nil {
			return err
		}
		if classifyInstallEntry(current, entry.Before, entry.After) != installEntryApplied {
			return fmt.Errorf(
				"file %s changed during installation recovery; keep the journal and resolve the edit",
				entry.Path,
			)
		}
		return RemoveRoot(files, relative)
	}
	source, err := files.Open(filepath.Join(backup, entry.Before.SHA256))
	if err != nil {
		return fmt.Errorf("open installation pre-image %s: %w", entry.Path, err)
	}
	hasher := sha256.New()
	check := func() error {
		if actual := hex.EncodeToString(hasher.Sum(nil)); actual != entry.Before.SHA256 {
			return fmt.Errorf(
				"installation pre-image %s for %s is incomplete",
				entry.Before.SHA256, entry.Path,
			)
		}
		current, err := observeInstallImage(files, relative)
		if err != nil {
			return err
		}
		if classifyInstallEntry(current, entry.Before, entry.After) != installEntryApplied {
			return fmt.Errorf(
				"file %s changed during installation recovery; keep the journal and resolve the edit",
				entry.Path,
			)
		}
		return nil
	}
	copyErr := fsutil.WriteRoot(
		files,
		relative,
		io.TeeReader(source, hasher),
		fsutil.WriteOptions{
			Mode:    os.FileMode(entry.Before.Mode),
			Replace: true,
			Check:   check,
		},
	)
	closeErr := source.Close()
	if copyErr != nil || closeErr != nil {
		return errors.Join(copyErr, closeErr)
	}
	return nil
}
