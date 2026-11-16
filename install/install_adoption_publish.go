package install

import (
	"errors"
	"fmt"
	"os"

	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/receipt"
)

// adoptionDocument is one metadata file the adoption transaction publishes.
// The plan binds the bytes to the before image the operation captured, so
// publication cannot merge into a document that changed after recovery.
type adoptionDocument struct {
	name string
	plan *installDocumentPlan
}

// publishAdoption journals the exact manifest, lock, and receipt bytes,
// applies them, rechecks every recorded file and reused document, and only
// then publishes the commit point. A failure before the commit point restores
// the pinned before images; recovery never overwrites an edit made after the
// interruption.
func publishAdoption(
	root string,
	opts AdoptOptions,
	next receipt.Receipt,
	publishReceipt bool,
	expectations []adoptionExpectation,
) (resultErr error) {
	pre := opts.Preconditions
	files, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, files.Close()) }()
	// Every pinned document must still match the recovered state, including
	// documents this transaction reuses without rewriting.
	for _, name := range []string{
		document.ManifestFilename,
		document.LockFilename,
		receipt.Filename,
	} {
		pinned, ok := pre.documents[name]
		if !ok {
			return fmt.Errorf("adoption preconditions do not pin %s", name)
		}
		if err := verifyPinnedCurrent(files, name, pinned); err != nil {
			return err
		}
	}

	var documents []adoptionDocument
	if opts.Manifest != nil {
		document, err := adoptionManifestDocument(root, opts)
		if err != nil {
			return err
		}
		documents = append(documents, document)
	}
	if opts.Lock != nil && opts.PublishLock {
		document, err := adoptionLockDocument(opts)
		if err != nil {
			return err
		}
		documents = append(documents, document)
	}
	if publishReceipt {
		pinned := pre.documents[receipt.Filename]
		data, err := next.Snapshot()
		if err != nil {
			return err
		}
		documents = append(documents, adoptionDocument{
			name: receipt.Filename,
			plan: pinnedPlan(receipt.Filename, pinned, 0o600, data),
		})
	}

	protected := make([]adoptionProtected, 0, len(pre.documents))
	for _, name := range []string{
		document.ManifestFilename,
		document.LockFilename,
		receipt.Filename,
	} {
		protected = append(protected, adoptionProtected{
			name:  name,
			image: pre.documents[name].image,
		})
	}
	return publishAdoptionDocuments(
		root,
		documents,
		expectations,
		protected,
		opts.Logger,
	)
}

// adoptionManifestDocument renders the manifest adoption publishes, merging
// into the recovered bytes when the caller updates a declaration in place.
func adoptionManifestDocument(
	root string,
	opts AdoptOptions,
) (adoptionDocument, error) {
	pinned := opts.Preconditions.documents[document.ManifestFilename]
	snapshot := opts.ManifestSnapshot
	if snapshot == nil {
		snapshot = func() ([]byte, error) {
			return document.ManifestFile(root).Snapshot(opts.Manifest)
		}
	}
	var (
		data []byte
		err  error
	)
	if pinned.exists && opts.ManifestMerge != nil {
		data, err = opts.ManifestMerge(pinned.raw)
	} else {
		data, err = snapshot()
	}
	if err != nil {
		return adoptionDocument{}, err
	}
	return adoptionDocument{
		name: document.ManifestFilename,
		plan: pinnedPlan(document.ManifestFilename, pinned, 0o644, data),
	}, nil
}

// adoptionLockDocument renders the resolved lock, merging into the recovered
// bytes when the workspace already had one.
func adoptionLockDocument(opts AdoptOptions) (adoptionDocument, error) {
	pinned := opts.Preconditions.documents[document.LockFilename]
	var (
		data []byte
		err  error
	)
	if pinned.exists {
		data, err = opts.Lock.MergeBytes(pinned.raw)
	} else {
		data, err = opts.Lock.Snapshot()
	}
	if err != nil {
		return adoptionDocument{}, err
	}
	return adoptionDocument{
		name: document.LockFilename,
		plan: pinnedPlan(document.LockFilename, pinned, 0o644, data),
	}, nil
}

// verifyPinnedCurrent rejects a document that changed after the operation
// captured its preconditions, so publication never overwrites external edits.
func verifyPinnedCurrent(
	files *os.Root,
	name string,
	pinned adoptionPinned,
) error {
	before, err := workspacewrite.ObserveInstallImage(files, name)
	if err != nil {
		return err
	}
	if !workspacewrite.InstallImagesEqual(before, pinned.image) {
		return fmt.Errorf(
			"adoption document %s changed after recovery; refusing to overwrite it",
			name,
		)
	}
	return nil
}

// pinnedPlan binds exact bytes to the before image captured for the document
// and the mode the publication preserves.
func pinnedPlan(
	name string,
	pinned adoptionPinned,
	defaultMode os.FileMode,
	data []byte,
) *installDocumentPlan {
	mode := defaultMode
	var base *workspacewrite.InstallImage
	if pinned.exists {
		// The captured mode already reflects the platform's permission
		// representation; reusing it preserves the file's permissions.
		mode = os.FileMode(pinned.image.Mode)
		base = pinned.image
	}
	return &installDocumentPlan{
		Data:  data,
		Base:  base,
		Image: *workspacewrite.InstallBytesImage(data, mode),
	}
}

func publishAdoptionDocuments(
	root string,
	documents []adoptionDocument,
	expected []adoptionExpectation,
	protected []adoptionProtected,
	logger *log.Logger,
) error {
	writes := make([]commitWrite, 0, len(documents))
	for _, document := range documents {
		after := document.plan.Image
		writes = append(writes, commitWrite{
			path:  document.name,
			data:  document.plan.Data,
			base:  document.plan.Base,
			after: &after,
		})
	}
	_, err := executeCommit(root, commitRequest{
		label:  "adoption",
		writes: writes,
		verify: func(
			files *os.Root,
			entries []workspacewrite.InstallJournalEntry,
		) error {
			if err := verifyAdoptedOutputs(root, expected); err != nil {
				return err
			}
			if err := recheckJournaledDocuments(files, entries); err != nil {
				return err
			}
			return recheckProtectedDocuments(files, protected, entries)
		},
	}, logger)
	return err
}

// recheckJournaledDocuments verifies the transaction's own writes immediately
// before the commit point.
func recheckJournaledDocuments(
	files *os.Root,
	entries []workspacewrite.InstallJournalEntry,
) error {
	for _, entry := range entries {
		current, err := workspacewrite.ObserveInstallImage(files, entry.Path)
		if err != nil {
			return err
		}
		if !workspacewrite.InstallImagesEqual(current, entry.After) {
			return fmt.Errorf(
				"adoption document %s changed before commit",
				entry.Path,
			)
		}
	}
	return nil
}

// recheckProtectedDocuments verifies the documents the transaction reuses as
// read-only commit preconditions, so an external edit cannot leave the
// published manifest and receipt describing a different lock.
func recheckProtectedDocuments(
	files *os.Root,
	protected []adoptionProtected,
	entries []workspacewrite.InstallJournalEntry,
) error {
	rewritten := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		rewritten[entry.Path] = struct{}{}
	}
	for _, pin := range protected {
		if _, ok := rewritten[pin.name]; ok {
			continue
		}
		current, err := workspacewrite.ObserveInstallImage(files, pin.name)
		if err != nil {
			return err
		}
		if !workspacewrite.InstallImagesEqual(current, pin.image) {
			return fmt.Errorf(
				"adoption document %s changed before commit; refusing to publish metadata that describes a different workspace",
				pin.name,
			)
		}
	}
	return nil
}
