package fsutil

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// WorkspaceMarker names the directory that marks a lucy workspace root.
const WorkspaceMarker = ".lucy"

// StagingRelative is the directory inside a workspace that holds staged copies.
// The install and cache stages share it, so staging names stay disjoint.
const StagingRelative = ".lucy/tmp"

// StagingPrefix names the staged copy of a publish.
const StagingPrefix = ".lucy-stage-"

// MaxStagedBase caps the destination basename embedded in a staging name.
const MaxStagedBase = 64

// staleStagingPrefixes name the staging copies written before this policy.
// They are inert; a sweep removes them so crash debris does not outlive the
// code that created it.
var staleStagingPrefixes = []string{".lucy-write-", ".lucy-file-"}

// RenameRoot publishes a closed, synced file inside root and persists the rename.
// Parent directories must exist. Platform implementations preserve root confinement.
func RenameRoot(root *os.Root, from, to string) error {
	return publishRoot(root, from, to, true)
}

// RenameRootNoReplace publishes only if the destination name is still absent.
func RenameRootNoReplace(root *os.Root, from, to string) error {
	return publishRoot(root, from, to, false)
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

// CopyFileAtomic discards incomplete copies and returns the published file at offset zero.
func CopyFileAtomic(reader io.Reader, path string, mode os.FileMode) (*os.File, error) {
	if err := atomicCopy(reader, path, mode); err != nil {
		return nil, err
	}
	return os.Open(path)
}

func atomicCopy(reader io.Reader, path string, mode os.FileMode) (resultErr error) {
	if err := MkdirAllPrivate(filepath.Dir(path)); err != nil {
		return fmt.Errorf("create file parent: %w", err)
	}
	rootDir, stageRel, destRel, err := stagingPlan(path)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(rootDir)
	if err != nil {
		return fmt.Errorf("open staging root: %w", err)
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
	if stageRel != "." {
		if err := root.MkdirAll(stageRel, 0o700); err != nil {
			return fmt.Errorf("create staging directory: %w", err)
		}
	}
	return publish(root, filepath.Join(stageRel, stagedName(filepath.Base(path))), destRel, reader, mode, true, nil)
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
		reader,
		opts.Mode,
		opts.Replace,
		opts.Check,
	)
}

// publish stages a payload and renames it into place. A reader therefore sees
// the previous content or the new content, never a partial write, and a stage
// that a crash abandons never masquerades as destination data.
func publish(
	root *os.Root,
	stage, dest string,
	reader io.Reader,
	mode os.FileMode,
	replace bool,
	check func() error,
) (resultErr error) {
	file, err := root.OpenFile(stage, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create staged file: %w", err)
	}
	defer func() {
		if file != nil {
			resultErr = errors.Join(resultErr, file.Close())
		}
		if err := root.Remove(stage); err != nil && !errors.Is(err, os.ErrNotExist) {
			resultErr = errors.Join(resultErr, fmt.Errorf("remove staged file: %w", err))
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
		file = nil
		return fmt.Errorf("close staged file: %w", err)
	}
	file = nil
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

// stagingPlan locates the staging root for a destination path: the nearest
// workspace containing it, so that the publish stays within one filesystem,
// or the destination's own directory when no workspace contains it. A
// workspace on another filesystem than the destination, such as a nested
// mount, stages beside the destination instead.
func stagingPlan(path string) (rootDir, stageRel, destRel string, err error) {
	dir := filepath.Dir(path)
	for {
		info, statErr := os.Stat(filepath.Join(dir, WorkspaceMarker))
		if statErr == nil {
			if !info.IsDir() {
				return "", "", "", fmt.Errorf("%s is not a directory", filepath.Join(dir, WorkspaceMarker))
			}
			relative, relErr := filepath.Rel(dir, path)
			if relErr != nil {
				return "", "", "", relErr
			}
			if sameDevice(filepath.Join(dir, StagingRelative), filepath.Dir(path)) {
				return dir, StagingRelative, relative, nil
			}
			return filepath.Dir(path), ".", filepath.Base(path), nil
		}
		if !errors.Is(statErr, os.ErrNotExist) {
			return "", "", "", statErr
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return filepath.Dir(path), ".", filepath.Base(path), nil
		}
		dir = parent
	}
}

// stagedName carries the destination basename so that a stage abandoned by a
// killed process names the file it belongs to.
func stagedName(base string) string {
	if len(base) > MaxStagedBase {
		base = base[:MaxStagedBase]
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

// IsStagingName reports whether name is a staged copy from the current policy
// or an earlier one. Crash debris carries either name, and both are inert.
func IsStagingName(name string) bool {
	if strings.HasPrefix(name, StagingPrefix) {
		return true
	}
	return slices.ContainsFunc(staleStagingPrefixes, func(prefix string) bool {
		return strings.HasPrefix(name, prefix)
	})
}
