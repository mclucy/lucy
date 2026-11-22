package install

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mclucy/lucy/bootstrap"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/receipt"
	serverpkg "github.com/mclucy/lucy/server"
)

func transactionTestDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func transactionTestWrite(t *testing.T, root, relative string, data []byte) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create parent for %s: %v", relative, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", relative, err)
	}
}

func transactionTestDocuments(t *testing.T, root string) WorkspaceDocuments {
	t.Helper()
	documents, err := CaptureWorkspaceDocuments(root)
	if err != nil {
		t.Fatalf("CaptureWorkspaceDocuments() error = %v", err)
	}
	return documents
}

// An accepted entry whose file is already absent must stay in the prune plan
// as a nil precondition. Dropping it from the plan would leave the journal and
// the pre-commit check blind to the path, so a file that reappeared would no
// longer fail the install.
func TestPruneTargetsKeepsAbsentEntryAsPrecondition(t *testing.T) {
	root := t.TempDir()
	ownedBytes := []byte("owned output\n")
	transactionTestWrite(t, root, "mods/present.jar", ownedBytes)
	previous := []receipt.Entry{
		{
			Path:   "mods/absent.jar",
			SHA256: transactionTestDigest([]byte("removed output\n")),
			Origin: receipt.OriginPackage,
		},
		{
			Path:   "mods/present.jar",
			SHA256: transactionTestDigest(ownedBytes),
			Origin: receipt.OriginPackage,
		},
	}

	targets, err := pruneTargets(
		root, previous, map[string]bool{}, nil, nil, false,
	)
	if err != nil {
		t.Fatalf("plan prunes: %v", err)
	}
	files, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	for _, path := range []string{"mods/absent.jar", "mods/present.jar"} {
		expected, planned := targets[path]
		if !planned {
			t.Fatalf("obsolete output %s has no guarded prune plan", path)
		}
		check := installBeforeCheck(files, workspacewrite.InstallJournalEntry{
			Path: path, Before: expected,
		})
		if err := check(); err != nil {
			t.Fatalf("unchanged prune target %s: %v", path, err)
		}
		transactionTestWrite(t, root, path, []byte("intervening user data"))
		if err := check(); err == nil {
			t.Fatalf("changed prune target %s was accepted", path)
		}
	}
}

// A commit that retains the previous bootstrap outputs because staging was
// skipped must revalidate them at the commit boundary. The currency check
// predates package staging and downloads, so a managed server output that
// drifts in that window must abort the commit before anything is written,
// instead of publishing a receipt that still reports bootstrap current.
func TestApplyCommitRetainedBootstrapDriftAborts(t *testing.T) {
	root := t.TempDir()
	core := []byte("plain server core\n")
	transactionTestWrite(t, root, "server/server.jar", core)
	lock := &document.Lock{
		SchemaVersion:       document.LockSchemaVersion,
		ManifestFingerprint: transactionTestDigest([]byte("manifest")),
		Server: document.LockedServer{
			Directory:   "server",
			Minecraft:   "1.21.4",
			Product:     serverpkg.ProductMinecraft,
			CoreVersion: "1.21.4",
			Bootstrap:   serverpkg.BootstrapMethodVanilla,
			Artifact: catalog.Artifact{
				Provider:  catalog.ProviderMojang,
				ProjectID: "server",
				Version:   "1.21.4",
				Filename:  "server.jar",
				Hashes:    map[string]string{"sha256": transactionTestDigest(core)},
			},
			Java: document.LockedJava{Advisory: 21},
		},
		Packages: []document.Package{},
	}
	fingerprint, err := receipt.BootstrapFingerprint(lock)
	if err != nil {
		t.Fatalf("bootstrap fingerprint: %v", err)
	}
	owned := receipt.New()
	owned.BootstrapFingerprint = fingerprint
	owned.Entries = []receipt.Entry{{
		Path:   "server/server.jar",
		SHA256: transactionTestDigest(core),
		Mode:   0o644,
		Origin: receipt.OriginBootstrap,
	}}
	ownedBytes, err := owned.Snapshot()
	if err != nil {
		t.Fatalf("receipt snapshot: %v", err)
	}
	receiptPath := filepath.Join(root, receipt.Filename)
	if err := os.MkdirAll(filepath.Dir(receiptPath), 0o755); err != nil {
		t.Fatalf("create receipt directory: %v", err)
	}
	if err := os.WriteFile(receiptPath, ownedBytes, 0o600); err != nil {
		t.Fatalf("write receipt: %v", err)
	}

	// Nothing was restaged, so the commit retains the bootstrap outputs; an
	// undrifted tree must still commit through the retained path.
	stage := t.TempDir()
	if _, err := applyCommit(
		root, stage, nil, lock, false, nil, nil, nil, false, installCommitOverlay, transactionTestDocuments(t, root), nil,
	); err != nil {
		t.Fatalf("overlay commit over current bootstrap: %v", err)
	}
	committed, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatalf("read committed receipt: %v", err)
	}

	// Drift after the currency check: the retained managed output disappears
	// while packages would be staging.
	if err := os.Remove(filepath.Join(root, "server", "server.jar")); err != nil {
		t.Fatalf("remove retained output: %v", err)
	}
	_, err = applyCommit(
		root, stage, nil, lock, false, nil, nil, nil, false, installCommitOverlay, transactionTestDocuments(t, root), nil,
	)
	if err == nil || !strings.Contains(err.Error(), "bootstrap") {
		t.Fatalf("commit over drifted retained bootstrap outputs: %v", err)
	}
	after, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatalf("reread receipt: %v", err)
	}
	if !bytes.Equal(committed, after) {
		t.Fatal("aborted commit still rewrote the receipt")
	}
}

// A staged seed has no ownership entry, so the receipt must record its path
// separately. A later install uses that record to notice a deleted seed and
// restage it without claiming the content.
func TestApplyCommitRecordsStagedSeedPaths(t *testing.T) {
	root := t.TempDir()
	core := []byte("plain server core\n")
	seed := []byte("-Xmx1G\n")
	lock := &document.Lock{
		SchemaVersion:       document.LockSchemaVersion,
		ManifestFingerprint: transactionTestDigest([]byte("manifest")),
		Server: document.LockedServer{
			Directory:   "server",
			Minecraft:   "1.21.4",
			Product:     serverpkg.ProductMinecraft,
			CoreVersion: "1.21.4",
			Bootstrap:   serverpkg.BootstrapMethodVanilla,
			Artifact: catalog.Artifact{
				Provider:  catalog.ProviderMojang,
				ProjectID: "server",
				Version:   "1.21.4",
				Filename:  "server.jar",
				Hashes:    map[string]string{"sha256": transactionTestDigest(core)},
			},
			Java: document.LockedJava{Advisory: 21},
		},
		Packages: []document.Package{},
	}
	stage := t.TempDir()
	transactionTestWrite(t, stage, "server/server.jar", core)
	transactionTestWrite(t, stage, "server/user_jvm_args.txt", seed)
	if err := os.MkdirAll(filepath.Join(root, ".lucy"), 0o700); err != nil {
		t.Fatalf("create state directory: %v", err)
	}
	stagedOutputs := map[string]bootstrap.Output{
		"server/server.jar": {
			Path: "server/server.jar", SHA256: transactionTestDigest(core),
			Mode: 0o644, Policy: bootstrap.PolicyManaged, Role: bootstrap.RoleRuntime,
		},
		"server/user_jvm_args.txt": {
			Path: "server/user_jvm_args.txt", SHA256: transactionTestDigest(seed),
			Mode: 0o644, Policy: bootstrap.PolicySeed, Role: bootstrap.RoleRuntime,
		},
	}
	if _, err := applyCommit(
		root, stage, nil, lock, false, nil, stagedOutputs, nil, true,
		installCommitReconcile, transactionTestDocuments(t, root), nil,
	); err != nil {
		t.Fatalf("commit staged bootstrap: %v", err)
	}
	committed, err := receipt.Read(root)
	if err != nil {
		t.Fatalf("read committed receipt: %v", err)
	}
	if !slices.Equal(committed.SeedPaths, []string{"server/user_jvm_args.txt"}) {
		t.Fatalf("seed paths = %v, want [server/user_jvm_args.txt]", committed.SeedPaths)
	}
	for _, entry := range committed.Entries {
		if entry.Path == "server/user_jvm_args.txt" {
			t.Fatal("seed was recorded as an owned entry")
		}
	}

	if err := os.Remove(filepath.Join(root, "server", "user_jvm_args.txt")); err != nil {
		t.Fatalf("remove seed: %v", err)
	}
	current, err := bootstrapCurrent(root, lock, committed, nil)
	if err != nil {
		t.Fatalf("bootstrap currentness: %v", err)
	}
	if current {
		t.Fatal("receipt with a deleted recorded seed reported current")
	}
}

func transactionTestImage(
	data []byte,
	mode os.FileMode,
) *workspacewrite.InstallImage {
	return workspacewrite.InstallBytesImage(data, mode)
}

func transactionTestRoot(t *testing.T, root string) *os.Root {
	t.Helper()
	files, err := os.OpenRoot(root)
	if err != nil {
		t.Fatalf("os.OpenRoot() error = %v", err)
	}
	t.Cleanup(func() {
		if err := files.Close(); err != nil {
			t.Errorf("os.Root.Close() error = %v", err)
		}
	})
	return files
}

// A captured document must be read through the workspace root instead of a
// path that a symlink or directory can redirect. The capture is the baseline
// every publication verifies against, so it may not follow a non-regular
// entry to another file.
func TestCaptureWorkspaceDocumentsRejectsNonRegularDocuments(t *testing.T) {
	t.Run("symlink", func(t *testing.T) {
		root := t.TempDir()
		outside := filepath.Join(t.TempDir(), "manifest.yaml")
		if err := os.WriteFile(outside, []byte("outside\n"), 0o644); err != nil {
			t.Fatalf("write outside document: %v", err)
		}
		if err := os.Symlink(
			outside,
			filepath.Join(root, document.ManifestFilename),
		); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if _, err := CaptureWorkspaceDocuments(root); err == nil {
			t.Fatal("CaptureWorkspaceDocuments() accepted a symlinked document")
		}
	})

	t.Run("directory", func(t *testing.T) {
		root := t.TempDir()
		if err := os.Mkdir(
			filepath.Join(root, document.ManifestFilename),
			0o755,
		); err != nil {
			t.Fatalf("create document directory: %v", err)
		}
		if _, err := CaptureWorkspaceDocuments(root); err == nil {
			t.Fatal("CaptureWorkspaceDocuments() accepted a directory as a document")
		}
	})
}

// Pinned documents must be rechecked after the caller's verifier has run.
// Caller verification may spend time hashing outputs, and an edit made during
// that work otherwise commits metadata that describes a different workspace.
// The recheck must also run when the transaction journals no changes.
func TestExecuteCommitRechecksDocumentsAfterVerifier(t *testing.T) {
	manifest := []byte("server:\n  directory: .\n")
	edited := []byte("server:\n  directory: .\n# edited while outputs were verified\n")
	editManifest := func(
		root string,
	) func(*os.Root, []workspacewrite.InstallJournalEntry) error {
		return func(*os.Root, []workspacewrite.InstallJournalEntry) error {
			return os.WriteFile(
				filepath.Join(root, document.ManifestFilename),
				edited,
				0o644,
			)
		}
	}

	t.Run("journaled write", func(t *testing.T) {
		root := t.TempDir()
		transactionTestWrite(
			t, root, document.ManifestFilename, manifest,
		)
		if err := os.MkdirAll(filepath.Join(root, ".lucy"), 0o755); err != nil {
			t.Fatalf("create workspace state directory: %v", err)
		}
		documents := transactionTestDocuments(t, root)
		published := []byte("runtime output\n")
		_, err := executeCommit(root, commitRequest{
			label: "test",
			writes: []commitWrite{{
				path:  "mods/example.jar",
				data:  published,
				after: transactionTestImage(published, 0o644),
			}},
			documents: documents,
			verify:    editManifest(root),
		}, nil)
		if err == nil || !strings.Contains(err.Error(), document.ManifestFilename) {
			t.Fatalf(
				"executeCommit() error = %v, want a pinned document recheck",
				err,
			)
		}
		raw, readErr := os.ReadFile(
			filepath.Join(root, document.ManifestFilename),
		)
		if readErr != nil {
			t.Fatalf("read externally edited manifest: %v", readErr)
		}
		if !bytes.Equal(raw, edited) {
			t.Fatal("aborted commit overwrote the external edit")
		}
		if _, statErr := os.Stat(
			filepath.Join(root, "mods", "example.jar"),
		); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("aborted commit left its journaled write: %v", statErr)
		}
	})

	t.Run("no journaled change", func(t *testing.T) {
		root := t.TempDir()
		transactionTestWrite(
			t, root, document.ManifestFilename, manifest,
		)
		documents := transactionTestDocuments(t, root)
		_, err := executeCommit(root, commitRequest{
			label:     "test",
			documents: documents,
			verify:    editManifest(root),
		}, nil)
		if err == nil || !strings.Contains(err.Error(), document.ManifestFilename) {
			t.Fatalf(
				"executeCommit() error = %v, want a pinned document recheck for a no-op commit",
				err,
			)
		}
		raw, readErr := os.ReadFile(
			filepath.Join(root, document.ManifestFilename),
		)
		if readErr != nil {
			t.Fatalf("read externally edited manifest: %v", readErr)
		}
		if !bytes.Equal(raw, edited) {
			t.Fatal("no-op commit overwrote the external edit")
		}
	})
}

// Paths the executor journals are skipped by its pre-write recheck, so the
// final verifier must compare a journaled write with its after image and a
// journaled prune with absence. Otherwise an edit made while another file was
// written leaves a receipt that no longer describes the workspace.
func TestVerifyInstallOutputsChecksJournaledPaths(t *testing.T) {
	published := []byte("published output\n")
	previous := []byte("previous output\n")
	publishedImage := transactionTestImage(published, 0o644)
	previousImage := transactionTestImage(previous, 0o644)

	t.Run("journaled write holds its after image", func(t *testing.T) {
		root := t.TempDir()
		transactionTestWrite(t, root, "mods/edited.jar", published)
		staged := map[string]commitWrite{
			"mods/edited.jar": {
				path:  "mods/edited.jar",
				after: publishedImage,
			},
		}
		entries := []workspacewrite.InstallJournalEntry{{
			Path:   "mods/edited.jar",
			Before: previousImage,
			After:  publishedImage,
		}}
		if err := verifyInstallOutputs(
			transactionTestRoot(t, root), entries, staged, nil, nil,
		); err != nil {
			t.Fatalf("unchanged journaled write rejected: %v", err)
		}
		transactionTestWrite(t, root, "mods/edited.jar", []byte("external edit\n"))
		if err := verifyInstallOutputs(
			transactionTestRoot(t, root), entries, staged, nil, nil,
		); err == nil || !strings.Contains(err.Error(), "mods/edited.jar") {
			t.Fatalf("edited journaled write accepted: %v", err)
		}
	})

	t.Run("journaled prune stays absent", func(t *testing.T) {
		root := t.TempDir()
		prunes := map[string]*workspacewrite.InstallImage{
			"mods/pruned.jar": previousImage,
		}
		entries := []workspacewrite.InstallJournalEntry{{
			Path:   "mods/pruned.jar",
			Before: previousImage,
		}}
		if err := verifyInstallOutputs(
			transactionTestRoot(t, root), entries, nil, prunes, nil,
		); err != nil {
			t.Fatalf("absent journaled prune rejected: %v", err)
		}
		transactionTestWrite(t, root, "mods/pruned.jar", previous)
		if err := verifyInstallOutputs(
			transactionTestRoot(t, root), entries, nil, prunes, nil,
		); err == nil || !strings.Contains(err.Error(), "mods/pruned.jar") {
			t.Fatalf("reappeared journaled prune accepted: %v", err)
		}
	})

	t.Run("planned output without a journal entry holds its image", func(t *testing.T) {
		root := t.TempDir()
		transactionTestWrite(t, root, "mods/unchanged.jar", published)
		staged := map[string]commitWrite{
			"mods/unchanged.jar": {
				path:  "mods/unchanged.jar",
				base:  publishedImage,
				after: publishedImage,
			},
		}
		if err := verifyInstallOutputs(
			transactionTestRoot(t, root), nil, staged, nil, nil,
		); err != nil {
			t.Fatalf("unchanged planned output rejected: %v", err)
		}
		transactionTestWrite(
			t, root, "mods/unchanged.jar", []byte("external edit\n"),
		)
		if err := verifyInstallOutputs(
			transactionTestRoot(t, root), nil, staged, nil, nil,
		); err == nil || !strings.Contains(err.Error(), "mods/unchanged.jar") {
			t.Fatalf("edited planned output accepted: %v", err)
		}
	})
}

// A receipt carried forward keeps claiming the entries it does not restage or
// prune. The final verifier must check those entries against their accepted
// baselines, honoring the legacy zero-mode wildcard, while a restaged or
// pruned path is checked against its planned result instead.
func TestVerifyInstallOutputsChecksRetainedEntries(t *testing.T) {
	core := []byte("server core\n")
	entry := receipt.Entry{
		Path:   "server/server.jar",
		SHA256: transactionTestDigest(core),
		Mode:   0o644,
		Origin: receipt.OriginBootstrap,
	}

	t.Run("retained output holds its baseline", func(t *testing.T) {
		root := t.TempDir()
		transactionTestWrite(t, root, entry.Path, core)
		check := func() error {
			return verifyInstallOutputs(
				transactionTestRoot(t, root),
				nil, nil, nil,
				[]receipt.Entry{entry},
			)
		}
		if err := check(); err != nil {
			t.Fatalf("unchanged retained output rejected: %v", err)
		}
		transactionTestWrite(t, root, entry.Path, []byte("drift\n"))
		if err := check(); err == nil || !strings.Contains(err.Error(), entry.Path) {
			t.Fatalf("changed retained output accepted: %v", err)
		}
	})

	t.Run("missing retained output fails", func(t *testing.T) {
		root := t.TempDir()
		if err := verifyInstallOutputs(
			transactionTestRoot(t, root), nil, nil, nil,
			[]receipt.Entry{entry},
		); err == nil || !strings.Contains(err.Error(), entry.Path) {
			t.Fatalf("missing retained output accepted: %v", err)
		}
	})

	t.Run("legacy zero mode stays a wildcard", func(t *testing.T) {
		root := t.TempDir()
		transactionTestWrite(t, root, entry.Path, core)
		legacy := entry
		legacy.Mode = 0
		if err := verifyInstallOutputs(
			transactionTestRoot(t, root), nil, nil, nil,
			[]receipt.Entry{legacy},
		); err != nil {
			t.Fatalf("legacy retained output rejected: %v", err)
		}
	})

	t.Run("restaged path uses its new image", func(t *testing.T) {
		root := t.TempDir()
		updated := []byte("updated wrapper\n")
		transactionTestWrite(t, root, "plugins/wrapper.py", updated)
		staged := map[string]commitWrite{
			"plugins/wrapper.py": {
				path:  "plugins/wrapper.py",
				after: transactionTestImage(updated, 0o644),
			},
		}
		stale := receipt.Entry{
			Path:   "plugins/wrapper.py",
			SHA256: transactionTestDigest([]byte("stale wrapper\n")),
			Mode:   0o644,
			Origin: receipt.OriginBootstrap,
		}
		if err := verifyInstallOutputs(
			transactionTestRoot(t, root), nil, staged, nil,
			[]receipt.Entry{stale},
		); err != nil {
			t.Fatalf("restaged path compared with its old baseline: %v", err)
		}
	})

	t.Run("pruned path uses absence", func(t *testing.T) {
		root := t.TempDir()
		previous := []byte("removed output\n")
		removed := receipt.Entry{
			Path:   "mods/removed.jar",
			SHA256: transactionTestDigest(previous),
			Mode:   0o644,
			Origin: receipt.OriginPackage,
		}
		prunes := map[string]*workspacewrite.InstallImage{
			removed.Path: transactionTestImage(previous, 0o644),
		}
		entries := []workspacewrite.InstallJournalEntry{{
			Path:   removed.Path,
			Before: transactionTestImage(previous, 0o644),
		}}
		if err := verifyInstallOutputs(
			transactionTestRoot(t, root), entries, nil, prunes,
			[]receipt.Entry{removed},
		); err != nil {
			t.Fatalf("absent pruned path rejected: %v", err)
		}
		transactionTestWrite(t, root, removed.Path, previous)
		if err := verifyInstallOutputs(
			transactionTestRoot(t, root), entries, nil, prunes,
			[]receipt.Entry{removed},
		); err == nil || !strings.Contains(err.Error(), removed.Path) {
			t.Fatalf("reappeared pruned path accepted: %v", err)
		}
	})
}
