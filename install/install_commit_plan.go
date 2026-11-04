package install

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/fsutil"
	"github.com/mclucy/lucy/internal/workspacewrite"
)

// installDocumentPlan binds the exact bytes predicted for a document
// publication to its journaled after image. Publication writes Data verbatim,
// so a concurrent comment edit cannot make published bytes differ from the
// journal.
type installDocumentPlan struct {
	Data  []byte
	Base  *workspacewrite.InstallImage
	Image workspacewrite.InstallImage
}

// installDocumentPlanFor predicts the bytes and mode a document write
// publishes. It mirrors document.Service.Write without re-reading the document
// at publication time.
func installDocumentPlanFor(
	root string,
	name string,
	defaultMode os.FileMode,
	snapshot func() ([]byte, error),
	merge func([]byte) ([]byte, error),
) (*installDocumentPlan, error) {
	path := filepath.Join(root, name)
	existing, err := os.ReadFile(path)
	switch {
	case err == nil:
		info, statErr := os.Lstat(path)
		if statErr != nil {
			return nil, statErr
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("write destination %s must be a regular file", name)
		}
		data, err := merge(existing)
		if err != nil {
			return nil, err
		}
		return &installDocumentPlan{
			Data:  data,
			Base:  installBytesImage(existing, info.Mode().Perm()),
			Image: *installBytesImage(data, info.Mode().Perm()),
		}, nil
	case errors.Is(err, os.ErrNotExist):
		data, err := snapshot()
		if err != nil {
			return nil, err
		}
		return &installDocumentPlan{
			Data:  data,
			Image: *installBytesImage(data, defaultMode),
		}, nil
	default:
		return nil, err
	}
}

func installFileMode(path string, defaultMode os.FileMode) (os.FileMode, error) {
	info, err := os.Lstat(path)
	switch {
	case err == nil:
		if !info.Mode().IsRegular() {
			return 0, fmt.Errorf("write destination %s must be a regular file", path)
		}
		return info.Mode().Perm(), nil
	case errors.Is(err, os.ErrNotExist):
		return defaultMode, nil
	default:
		return 0, err
	}
}

func installBytesImage(data []byte, mode os.FileMode) *workspacewrite.InstallImage {
	sum := sha256.Sum256(data)
	return &workspacewrite.InstallImage{
		SHA256: hex.EncodeToString(sum[:]),
		Mode:   effectiveInstallMode(mode),
	}
}

// effectiveInstallMode reports the permission bits the platform exposes after
// a chmod to mode. Windows represents regular files as writable (0666) or
// read-only (0444); Unix preserves the requested bits.
func effectiveInstallMode(mode os.FileMode) uint32 {
	if runtime.GOOS == "windows" {
		if mode&0o200 != 0 {
			return 0o666
		}
		return 0o444
	}
	return uint32(mode.Perm())
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
		current, err := observeInstallPath(files, entry.Path)
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

func observeInstallPath(
	files *os.Root,
	relative string,
) (*workspacewrite.InstallImage, error) {
	info, err := files.Lstat(relative)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf(
			"installation output %s is not a regular file",
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
	return &workspacewrite.InstallImage{
		SHA256: hex.EncodeToString(hasher.Sum(nil)),
		Mode:   effectiveInstallMode(info.Mode().Perm()),
	}, nil
}

func installFinalImageMatches(
	current, expected *workspacewrite.InstallImage,
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

func installPathReserved(root, name string) bool {
	for _, reserved := range []string{
		workspacewrite.InstallJournalName,
		document.LockFilename,
		document.ManifestFilename,
	} {
		if installPathMatchesReserved(root, name, reserved) {
			return true
		}
	}
	return false
}

// installPathMatchesReserved rejects an exact reserved path always, and a case
// variant only when the target filesystem treats the two names as one entry.
func installPathMatchesReserved(root, name, reserved string) bool {
	if name == reserved {
		return true
	}
	if !strings.EqualFold(name, reserved) {
		return false
	}
	return workspacewrite.InstallPathsAlias(root, reserved, name)
}

// rejectInstallPathAliases refuses names that the target filesystem resolves
// to the same entry. Otherwise one journal entry could overwrite and another
// could prune the same file. Distinct case-sensitive paths stay valid.
func rejectInstallPathAliases(root string, names []string) error {
	aliases := make(map[string][]string, len(names))
	for _, name := range names {
		if !filepath.IsLocal(filepath.FromSlash(name)) ||
			filepath.ToSlash(filepath.Clean(filepath.FromSlash(name))) != name {
			return fmt.Errorf("installation path %q is not canonical", name)
		}
		key := strings.ToLower(name)
		for _, prior := range aliases[key] {
			if workspacewrite.InstallPathsAlias(root, prior, name) {
				return fmt.Errorf(
					"installation paths %q and %q alias the same filesystem entry",
					prior, name,
				)
			}
		}
		aliases[key] = append(aliases[key], name)
	}
	return nil
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
			return fmt.Errorf("installation pre-image %s is not a regular file", image.SHA256)
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
