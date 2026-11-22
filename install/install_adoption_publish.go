package install

import (
	"os"

	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/receipt"
)

// publishAdoption rechecks every claimed output, every seed the bootstrap
// fingerprint requires, and every reused document before the journal commit
// point, so a concurrent edit cannot enter the receipt as an accepted
// baseline.
func publishAdoption(
	root string,
	opts AdoptOptions,
	next receipt.Receipt,
	publishReceipt bool,
	expected map[string]*workspacewrite.InstallImage,
	seeds []string,
	check currentCheck,
) error {
	manifestSnapshot := opts.ManifestSnapshot
	if opts.Manifest != nil && manifestSnapshot == nil {
		manifestSnapshot = func() ([]byte, error) {
			return document.ManifestFile(root).Snapshot(opts.Manifest)
		}
	}
	writes, err := opts.Preconditions.documentWrites(
		documentCandidate{
			name:     document.ManifestFilename,
			mode:     0o644,
			publish:  opts.Manifest != nil,
			snapshot: manifestSnapshot,
			merge:    opts.ManifestMerge,
		},
		documentCandidate{
			name:     document.LockFilename,
			mode:     0o644,
			publish:  opts.Lock != nil && opts.PublishLock,
			snapshot: opts.Lock.Snapshot,
			merge:    opts.Lock.MergeBytes,
		},
		documentCandidate{
			name:     receipt.Filename,
			mode:     0o600,
			publish:  publishReceipt,
			snapshot: next.Snapshot,
		},
	)
	if err != nil {
		return err
	}
	_, err = executeCommit(root, commitRequest{
		label:     "adoption",
		writes:    writes,
		documents: opts.Preconditions,
		verify: func(
			files *os.Root,
			_ []workspacewrite.InstallJournalEntry,
		) error {
			if err := verifyAdoptedOutputs(files, root, expected, check); err != nil {
				return err
			}
			return verifyAdoptedSeeds(files, seeds)
		},
	}, check, opts.Logger)
	if err != nil {
		return err
	}
	check.evidence.write(root, next.Entries, opts.Logger)
	return nil
}
