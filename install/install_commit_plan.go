package install

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/fileidentity"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/receipt"
)

// WorkspaceDocuments pins the manifest, lock, and receipt bytes an operation
// observed after journal recovery. Capture it under the workspace writer lock
// and run recovery first, so the captured state is the recovered one. A nil
// documents map is the zero value and means nothing was captured.
type WorkspaceDocuments struct {
	documents map[string]workspaceDocument
}

// workspaceDocument is one document state captured before planning. A nil
// image records an absent path, which is distinct from a present empty file.
type workspaceDocument struct {
	raw   []byte
	image *workspacewrite.InstallImage
}

// workspaceDocumentNames lists the documents a capture pins, in stable order.
var workspaceDocumentNames = [...]string{
	document.ManifestFilename,
	document.LockFilename,
	receipt.Filename,
}

// CaptureWorkspaceDocuments records the current state of the documents an
// operation may publish. The caller must hold the workspace writer lock and
// must run journal recovery first, so the captured state is the recovered one.
func CaptureWorkspaceDocuments(root string) (captured WorkspaceDocuments, resultErr error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return WorkspaceDocuments{}, err
	}
	files, err := os.OpenRoot(root)
	if err != nil {
		return WorkspaceDocuments{}, err
	}
	defer func() { resultErr = errors.Join(resultErr, files.Close()) }()
	captured.documents = make(map[string]workspaceDocument, len(workspaceDocumentNames))
	for _, name := range workspaceDocumentNames {
		pinned, err := captureWorkspaceDocument(files, root, name)
		if err != nil {
			return WorkspaceDocuments{}, err
		}
		captured.documents[name] = pinned
	}
	return captured, nil
}

// captureWorkspaceDocument opens one document through the workspace root and
// reads the bytes and permission mode from that same handle, so the capture
// cannot pair bytes from one file with metadata from another. The root
// confines the open, and a replaced entry fails the identity recheck.
func captureWorkspaceDocument(
	files *os.Root,
	root string,
	name string,
) (workspaceDocument, error) {
	if err := workspacewrite.SafePath(root, name); err != nil {
		return workspaceDocument{}, err
	}
	info, err := files.Lstat(name)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return workspaceDocument{}, nil
	case err != nil:
		return workspaceDocument{}, err
	}
	if !info.Mode().IsRegular() {
		return workspaceDocument{}, fmt.Errorf(
			"workspace document %s is not a regular file",
			name,
		)
	}
	file, err := files.Open(name)
	if err != nil {
		return workspaceDocument{}, err
	}
	opened, err := file.Stat()
	if err != nil {
		return workspaceDocument{}, errors.Join(err, file.Close())
	}
	if !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return workspaceDocument{}, errors.Join(
			fmt.Errorf("workspace document %s changed while capturing it", name),
			file.Close(),
		)
	}
	raw, err := io.ReadAll(file)
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		return workspaceDocument{}, errors.Join(err, closeErr)
	}
	return workspaceDocument{
		raw:   raw,
		image: workspacewrite.InstallBytesImage(raw, opened.Mode().Perm()),
	}, nil
}

// documentWrite predicts the bytes and mode publishing a document produces
// from the captured state, without re-reading the document at publication
// time. A present document merges into its captured bytes; an absent one is
// encoded by snapshot. Publication preserves a present document's mode.
func (d WorkspaceDocuments) documentWrite(
	name string,
	defaultMode os.FileMode,
	snapshot func() ([]byte, error),
	merge func([]byte) ([]byte, error),
) (commitWrite, error) {
	captured, ok := d.documents[name]
	if !ok {
		return commitWrite{}, fmt.Errorf(
			"workspace document %s was not captured",
			name,
		)
	}
	var (
		data []byte
		err  error
	)
	if captured.image != nil && merge != nil {
		data, err = merge(captured.raw)
	} else {
		if snapshot == nil {
			return commitWrite{}, fmt.Errorf(
				"workspace document %s has no snapshot",
				name,
			)
		}
		data, err = snapshot()
	}
	if err != nil {
		return commitWrite{}, err
	}
	mode := defaultMode
	if captured.image != nil {
		// The captured mode already reflects the platform's permission
		// representation; reusing it preserves the document's permissions.
		mode = os.FileMode(captured.image.Mode)
	}
	after := workspacewrite.InstallBytesImage(data, mode)
	return commitWrite{
		path:  name,
		data:  data,
		base:  captured.image,
		after: after,
	}, nil
}

// documentCandidate is one metadata file a publication may rewrite. publish
// false keeps the candidate out of the transaction while still declaring its
// mode and encoders.
type documentCandidate struct {
	name     string
	mode     os.FileMode
	publish  bool
	snapshot func() ([]byte, error)
	merge    func([]byte) ([]byte, error)
}

// documentWrites plans the write for every published candidate, in declaration
// order, so adoption and installation share one publication convention.
func (d WorkspaceDocuments) documentWrites(
	candidates ...documentCandidate,
) ([]commitWrite, error) {
	writes := make([]commitWrite, 0, len(candidates))
	for _, candidate := range candidates {
		if !candidate.publish {
			continue
		}
		write, err := d.documentWrite(
			candidate.name,
			candidate.mode,
			candidate.snapshot,
			candidate.merge,
		)
		if err != nil {
			return nil, err
		}
		writes = append(writes, write)
	}
	return writes, nil
}

// verify rechecks every pinned document against files. A document named by
// writes is compared with the image its publication produces, including a
// write skipped because the bytes already matched; every other document is
// compared with its captured image. An external edit, mode change, or
// appearance/disappearance fails closed so publication never overwrites it.
func (d WorkspaceDocuments) verify(
	files *os.Root,
	writes []commitWrite,
	memo *fileidentity.Memo,
) error {
	if d.documents == nil {
		return nil
	}
	for _, name := range workspaceDocumentNames {
		captured, ok := d.documents[name]
		if !ok {
			return fmt.Errorf("workspace documents do not pin %s", name)
		}
		expected := captured.image
		for _, write := range writes {
			if write.path == name {
				expected = write.after
				break
			}
		}
		current, err := workspacewrite.ObserveInstallImageCached(files, name, memo)
		if err != nil {
			return err
		}
		if !workspacewrite.InstallImagesEqual(current, expected) {
			return fmt.Errorf(
				"workspace document %s changed since capture; refusing to publish metadata that describes a different workspace",
				name,
			)
		}
	}
	return nil
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
