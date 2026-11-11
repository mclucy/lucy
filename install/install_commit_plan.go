package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/document"
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
			Base:  workspacewrite.InstallBytesImage(existing, info.Mode().Perm()),
			Image: *workspacewrite.InstallBytesImage(data, info.Mode().Perm()),
		}, nil
	case errors.Is(err, os.ErrNotExist):
		data, err := snapshot()
		if err != nil {
			return nil, err
		}
		return &installDocumentPlan{
			Data:  data,
			Image: *workspacewrite.InstallBytesImage(data, defaultMode),
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

// rejectCommitPathAliases refuses names the target filesystem resolves to the
// same entry, and names that are not canonical. Otherwise one journal entry
// could overwrite and another could prune the same file. Distinct
// case-sensitive paths stay valid.
func rejectCommitPathAliases(root, label string, names []string) error {
	aliases := make(map[string][]string, len(names))
	for _, name := range names {
		if !filepath.IsLocal(filepath.FromSlash(name)) ||
			filepath.ToSlash(filepath.Clean(filepath.FromSlash(name))) != name {
			return fmt.Errorf("%s path %q is not canonical", label, name)
		}
		key := strings.ToLower(name)
		for _, prior := range aliases[key] {
			if workspacewrite.InstallPathsAlias(root, prior, name) {
				return fmt.Errorf(
					"%s paths %q and %q alias the same filesystem entry",
					label,
					prior,
					name,
				)
			}
		}
		aliases[key] = append(aliases[key], name)
	}
	return nil
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
