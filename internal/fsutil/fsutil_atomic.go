package fsutil

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// WorkspaceMarker names the directory that marks a lucy workspace root.
const WorkspaceMarker = ".lucy"

// StagingRelative is the directory inside a workspace that holds staged copies
// written through WriteRoot while the workspace writer lock is held.
const StagingRelative = ".lucy/tmp"

// StagingPrefix names the staged copy of a publish.
const StagingPrefix = ".lucy-stage-"

// MaxStagedBase caps the destination basename embedded in a staging name.
const MaxStagedBase = 64

// RenameRoot publishes a closed, synced file inside root and persists the rename.
// Parent directories must exist. Platform implementations preserve root confinement.
func RenameRoot(root *os.Root, from, to string) error {
	return publishRoot(root, from, to, true)
}

// RenameRootNoReplace publishes only if the destination name is still absent.
func RenameRootNoReplace(root *os.Root, from, to string) error {
	return publishRoot(root, from, to, false)
}

// RenameDirRoot renames a directory inside root and persists the rename. The
// destination must be absent. The directory's own entries must already be
// durable.
func RenameDirRoot(root *os.Root, from, to string) error {
	if !filepath.IsLocal(from) || !filepath.IsLocal(to) {
		return fmt.Errorf("rename paths must stay inside their root")
	}
	return renameDirRoot(root, filepath.Clean(from), filepath.Clean(to))
}

func publishRoot(root *os.Root, from, to string, replace bool) error {
	if !filepath.IsLocal(from) || !filepath.IsLocal(to) {
		return fmt.Errorf("rename paths must stay inside their root")
	}
	from = filepath.Clean(from)
	to = filepath.Clean(to)
	for _, name := range []string{from, to} {
		info, err := root.Lstat(name)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err == nil && !info.Mode().IsRegular() {
			return fmt.Errorf("rename endpoint must be a regular file")
		}
		for parent := filepath.Dir(name); parent != "."; parent = filepath.Dir(parent) {
			info, err := root.Lstat(parent)
			if err != nil {
				return err
			}
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("rename parent must be a regular directory")
			}
		}
	}
	return renameRoot(root, from, to, replace)
}

// AtomicWrite replaces a file atomically and retains its permissions.
// New files use defaultMode.
func AtomicWrite(path string, data []byte, defaultMode os.FileMode) error {
	mode := defaultMode
	info, err := os.Lstat(path)
	if err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("write destination must be a regular file")
		}
		mode = info.Mode().Perm()
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect file permissions: %w", err)
	}
	return atomicCopy(bytes.NewReader(data), path, mode)
}

func atomicCopy(reader io.Reader, path string, mode os.FileMode) (resultErr error) {
	if err := MkdirAllPrivate(filepath.Dir(path)); err != nil {
		return fmt.Errorf("create file parent: %w", err)
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("open file parent: %w", err)
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
	base := filepath.Base(path)
	return publish(root, stagedName(base), base, func(stage string) error {
		return writeStage(root, stage, reader, mode)
	}, true, nil)
}

// WriteOptions controls one publish.
type WriteOptions struct {
	// Mode is the permission set applied to the published file.
	Mode os.FileMode
	// Replace publishes over an existing destination. Without it the publish
	// succeeds only while the destination name is still absent, so a file that
	// appears during provisioning survives.
	Replace bool
	// Check runs after the payload is staged and immediately before
	// publication, so a caller can abandon a publish whose destination the
	// world changed while the payload was being staged.
	Check func() error
}

// WriteRoot publishes a payload at a path confined to root. The payload is
// staged inside the workspace staging directory when root is a workspace and
// beside the destination otherwise, so a publish never crosses a filesystem.
func WriteRoot(root *os.Root, relative string, reader io.Reader, opts WriteOptions) error {
	if !filepath.IsLocal(relative) {
		return fmt.Errorf("output path must stay inside the workspace")
	}
	parent := filepath.Dir(relative)
	if err := root.MkdirAll(parent, 0o700); err != nil {
		return err
	}
	stageRel, err := stagingRelative(root, parent)
	if err != nil {
		return err
	}
	return publish(
		root,
		filepath.Join(stageRel, stagedName(filepath.Base(relative))),
		relative,
		func(stage string) error {
			return writeStage(root, stage, reader, opts.Mode)
		},
		opts.Replace,
		opts.Check,
	)
}

// WriteFileRoot publishes the content of src at a path confined to root under
// the same staging rules as WriteRoot. link selects how the bytes reach the
// staged name: a hardlink or a clone never copies them.
func WriteFileRoot(
	root *os.Root,
	relative, src string,
	link LinkMode,
	opts WriteOptions,
) error {
	if !filepath.IsLocal(relative) {
		return fmt.Errorf("output path must stay inside the workspace")
	}
	parent := filepath.Dir(relative)
	if err := root.MkdirAll(parent, 0o700); err != nil {
		return err
	}
	stageRel, err := stagingRelative(root, parent)
	if err != nil {
		return err
	}
	return publish(
		root,
		filepath.Join(stageRel, stagedName(filepath.Base(relative))),
		relative,
		func(stage string) error {
			return stageFile(root, stage, src, opts.Mode, link)
		},
		opts.Replace,
		opts.Check,
	)
}

// stageFile places the content of src at the staged name inside root. A
// hardlink shares the source inode, so it applies only while src already
// carries mode; a clone takes mode; a byte copy writes the bytes. The staged
// directory is opened through root, so a path change cannot move the write
// outside root. The staged name stays unpublished until the caller renames it.
func stageFile(root *os.Root, stage, src string, mode os.FileMode, link LinkMode) (resultErr error) {
	dir, err := root.Open(filepath.Dir(stage))
	if err != nil {
		return fmt.Errorf("open staging directory: %w", err)
	}
	defer func() { resultErr = errors.Join(resultErr, dir.Close()) }()
	if placeFileAt(dir, filepath.Base(stage), src, mode.Perm(), link) {
		if err := syncStaged(root, stage); err != nil {
			return errors.Join(err, removeStaged(root, stage))
		}
		return nil
	}
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, source.Close()) }()
	return writeStage(root, stage, source, mode)
}

// syncStaged makes linked or cloned content durable before publication. A
// link or a clone writes no bytes through a descriptor, and the source can be
// a stage file that was never synced, so the staged inode needs its own sync.
func syncStaged(root *os.Root, stage string) error {
	file, err := root.Open(stage)
	if err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return errors.Join(fmt.Errorf("sync staged file: %w", err), file.Close())
	}
	return file.Close()
}

// writeStage writes reader into the staged file inside root, applies mode, and
// makes the content durable before publication. A failure removes the partial
// file.
func writeStage(root *os.Root, stage string, reader io.Reader, mode os.FileMode) (resultErr error) {
	file, err := root.OpenFile(stage, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create staged file: %w", err)
	}
	complete := false
	defer func() {
		if !complete {
			resultErr = errors.Join(resultErr, removeStaged(root, stage))
		}
	}()
	if err := fillStage(file, reader, mode); err != nil {
		return err
	}
	complete = true
	return nil
}

// fillStage writes reader into an open staged file, applies mode, makes the
// content durable, and closes the file. It closes the file even on failure.
func fillStage(file *os.File, reader io.Reader, mode os.FileMode) (resultErr error) {
	closed := false
	defer func() {
		if !closed {
			resultErr = errors.Join(resultErr, file.Close())
		}
	}()
	if _, err := io.Copy(file, reader); err != nil {
		return fmt.Errorf("write staged file: %w", err)
	}
	if err := file.Chmod(mode.Perm()); err != nil {
		return fmt.Errorf("set staged permissions: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync staged file: %w", err)
	}
	if err := file.Close(); err != nil {
		closed = true
		return fmt.Errorf("close staged file: %w", err)
	}
	closed = true
	return nil
}

// publish stages a payload through stagePayload and renames it into place. A
// reader therefore sees the previous content or the new content, never a
// partial write, and a stage that a crash abandons never masquerades as
// destination data. A failure after staging removes the staged name, and a
// payload that fails while being staged removes its own partial file.
func publish(
	root *os.Root,
	stage, dest string,
	stagePayload func(stage string) error,
	replace bool,
	check func() error,
) (resultErr error) {
	if err := stagePayload(stage); err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, removeStaged(root, stage)) }()
	if check != nil {
		if err := check(); err != nil {
			return err
		}
	}
	if replace {
		return RenameRoot(root, stage, dest)
	}
	return RenameRootNoReplace(root, stage, dest)
}

// removeStaged deletes one staged name and tolerates a name that a completed
// publish already renamed away.
func removeStaged(root *os.Root, stage string) error {
	if err := root.Remove(stage); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove staged file: %w", err)
	}
	return nil
}

// stagingRelative prefers the workspace staging directory, which keeps a
// staged payload out of the destination namespace, and falls back to the
// destination's own directory for a root that is not a workspace, or when the
// staging directory sits on another filesystem than the destination.
func stagingRelative(root *os.Root, parent string) (string, error) {
	info, err := root.Stat(WorkspaceMarker)
	if err == nil {
		if !info.IsDir() {
			return "", fmt.Errorf("%s is not a directory", WorkspaceMarker)
		}
		if err := root.MkdirAll(StagingRelative, 0o700); err != nil {
			return "", fmt.Errorf("create staging directory: %w", err)
		}
		if sameDevice(filepath.Join(root.Name(), StagingRelative), filepath.Join(root.Name(), parent)) {
			return StagingRelative, nil
		}
		return parent, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return parent, nil
}

// stagedName carries the destination basename so that a stage abandoned by a
// killed process names the file it belongs to.
func stagedName(base string) string {
	if len(base) > MaxStagedBase {
		cut := MaxStagedBase
		for cut > 0 && !utf8.RuneStart(base[cut]) {
			cut--
		}
		base = base[:cut]
	}
	return StagingPrefix + base + "-" + rand.Text()
}

// SweepStaging removes staging copies that a killed publisher abandoned. The
// caller holds the workspace writer lock, which proves that no publisher is
// live and that every staging copy found is debris.
func SweepStaging(root *os.Root) (removed []string, resultErr error) {
	dir, err := root.Open(StagingRelative)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open staging directory: %w", err)
	}
	entries, err := dir.ReadDir(-1)
	resultErr = errors.Join(err, dir.Close())
	if err != nil {
		return nil, fmt.Errorf("read staging directory: %w", resultErr)
	}
	for _, entry := range entries {
		if entry.IsDir() || !IsStagingName(entry.Name()) {
			continue
		}
		name := filepath.Join(StagingRelative, entry.Name())
		if err := root.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
			resultErr = errors.Join(resultErr, fmt.Errorf("remove staged file: %w", err))
			continue
		}
		removed = append(removed, name)
	}
	return removed, resultErr
}

// IsStagingName reports whether name is a staged copy from the current policy.
// Crash debris carries such a name and is inert.
func IsStagingName(name string) bool {
	return strings.HasPrefix(name, StagingPrefix)
}
