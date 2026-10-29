package filetrack

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/mclucy/lucy/cas"
	"github.com/mclucy/lucy/catalog"
	documentpkg "github.com/mclucy/lucy/document"
	serverpkg "github.com/mclucy/lucy/server"
	"gopkg.in/yaml.v3"
)

// behaviorWorkspace returns an isolated workspace with the manifest every
// transition test starts from. The caller owns every file below root.
func behaviorWorkspace(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	behaviorWriteManifest(t, root, behaviorManifest())
	return root
}

func behaviorManifest() *documentpkg.Manifest {
	return &documentpkg.Manifest{
		SchemaVersion: documentpkg.ManifestSchemaVersion,
		Server: documentpkg.ManifestServer{
			Minecraft: "1.21.4",
			Directory: "server",
			Core:      documentpkg.ManifestCore{Product: serverpkg.ProductMinecraft},
		},
	}
}

func behaviorWriteManifest(t *testing.T, root string, document *documentpkg.Manifest) string {
	t.Helper()
	if err := documentpkg.ManifestFile(root).Write(document); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	fingerprint, err := document.Fingerprint()
	if err != nil {
		t.Fatalf("fingerprint manifest: %v", err)
	}
	return fingerprint
}

func behaviorService() *Service {
	return NewService(&http.Client{}, nil)
}

func behaviorWrite(t *testing.T, root, relative string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := behaviorWriteFile(filepath.Join(root, relative), data, mode); err != nil {
		t.Fatalf("write %s: %v", relative, err)
	}
}

func behaviorWriteFile(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, mode)
}

func behaviorRead(t *testing.T, root, relative string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, relative))
	if err != nil {
		t.Fatalf("read %s: %v", relative, err)
	}
	return data
}

func behaviorRequireAbsent(t *testing.T, root, relative string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(root, relative)); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be absent, got %v", relative, err)
	}
}

func behaviorRequireBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	actual, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(actual) != string(want) {
		t.Fatalf("%s holds %q, want %q", path, actual, want)
	}
}

func behaviorDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func behaviorHex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func behaviorRequireObject(t *testing.T, root, hash string, want []byte) {
	t.Helper()
	digest, err := cas.ParseDigest(hash)
	if err != nil {
		t.Fatalf("parse object digest %q: %v", hash, err)
	}
	hexDigest := digest.Hex()
	path := filepath.Join(root, ".lucy", "objects", "sha256", hexDigest[:2], hexDigest)
	behaviorRequireBytes(t, path, want)
}

func behaviorDropObject(t *testing.T, root, hash string) {
	t.Helper()
	digest, err := cas.ParseDigest(hash)
	if err != nil {
		t.Fatalf("parse object digest %q: %v", hash, err)
	}
	hexDigest := digest.Hex()
	path := filepath.Join(root, ".lucy", "objects", "sha256", hexDigest[:2], hexDigest)
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove object %s: %v", hash, err)
	}
}

func behaviorRun(t *testing.T, root string, request Request) Result {
	t.Helper()
	result, err := behaviorService().Run(t.Context(), root, request)
	if err != nil {
		t.Fatalf("file %s %v: %v", request.Operation, request.Paths, err)
	}
	return result
}

func behaviorRunErr(t *testing.T, root string, request Request) error {
	t.Helper()
	_, err := behaviorService().Run(t.Context(), root, request)
	if err == nil {
		t.Fatalf("file %s %v succeeded, want an error", request.Operation, request.Paths)
	}
	return err
}

func behaviorSoleChange(t *testing.T, result Result) Change {
	t.Helper()
	if len(result.Event.Changes) != 1 {
		t.Fatalf("result changes = %+v, want one", result.Event.Changes)
	}
	return result.Event.Changes[0]
}

func behaviorDeclared(t *testing.T, root string) []documentpkg.TrackedFile {
	t.Helper()
	files, err := DeclaredFiles(root)
	if err != nil {
		t.Fatalf("DeclaredFiles() error = %v", err)
	}
	return files
}

func behaviorStatus(t *testing.T, root, path string) Status {
	t.Helper()
	statuses, err := Inspect(root, behaviorDeclared(t, root))
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	for _, status := range statuses {
		if status.Path == path {
			return status
		}
	}
	t.Fatalf("Inspect() does not declare %s: %+v", path, statuses)
	return Status{}
}

func behaviorHistory(t *testing.T, root, path string) []Event {
	t.Helper()
	events, err := History(root, path)
	if err != nil {
		t.Fatalf("History(%q) error = %v", path, err)
	}
	return events
}

func TestFileTrackPublishesAcceptedRevision(t *testing.T) {
	root := behaviorWorkspace(t)
	rules := []byte("keep the world border at 3000\n")
	behaviorWrite(t, root, "config/rules.txt", rules, 0o644)

	result := behaviorRun(t, root, Request{Operation: Track, Paths: []string{"config/rules.txt"}})
	change := behaviorSoleChange(t, result)
	if result.Event.Sequence != 1 || result.Event.Operation != Track {
		t.Fatalf("unexpected history event %+v", result.Event)
	}
	if !change.Accepted || change.Revision != 1 || change.Parent != 0 {
		t.Fatalf("unexpected accepted revision %+v", change)
	}
	if change.After == nil || change.After.Hash != behaviorDigest(rules) || change.After.Immutable {
		t.Fatalf("unexpected tracked declaration %+v", change.After)
	}
	declared := behaviorDeclared(t, root)
	if len(declared) != 1 || declared[0].Path != "config/rules.txt" ||
		declared[0].Hash != behaviorDigest(rules) || declared[0].Immutable {
		t.Fatalf("manifest declares %+v", declared)
	}
	behaviorRequireObject(t, root, behaviorDigest(rules), rules)

	status := behaviorStatus(t, root, "config/rules.txt")
	if status.State != "matching" || status.Hash != behaviorDigest(rules) ||
		status.ActualHash != behaviorDigest(rules) {
		t.Fatalf("unexpected live status %+v", status)
	}
	events := behaviorHistory(t, root, "")
	if len(events) != 1 || events[0].Sequence != 1 {
		t.Fatalf("history holds %+v", events)
	}
}

func TestFileTrackRejectsAlreadyTrackedPathAtomically(t *testing.T) {
	root := behaviorWorkspace(t)
	rules := []byte("rules\n")
	behaviorWrite(t, root, "config/rules.txt", rules, 0o644)
	behaviorRun(t, root, Request{Operation: Track, Paths: []string{"config/rules.txt"}})
	manifestBytes := behaviorRead(t, root, documentpkg.ManifestFilename)

	behaviorRunErr(t, root, Request{Operation: Track, Paths: []string{"config/rules.txt"}})

	behaviorRequireBytes(t, filepath.Join(root, documentpkg.ManifestFilename), manifestBytes)
	if events := behaviorHistory(t, root, ""); len(events) != 1 {
		t.Fatalf("rejected track left %d history events, want 1", len(events))
	}
}

func TestFileUpdateAcceptsFreshRevisionWithParent(t *testing.T) {
	root := behaviorWorkspace(t)
	first := []byte("rules v1\n")
	second := []byte("rules v2\n")
	behaviorWrite(t, root, "config/rules.txt", first, 0o644)
	behaviorRun(t, root, Request{Operation: Track, Paths: []string{"config/rules.txt"}})
	behaviorWrite(t, root, "config/rules.txt", second, 0o644)

	change := behaviorSoleChange(t, behaviorRun(t, root, Request{
		Operation: Update,
		Paths:     []string{"config/rules.txt"},
	}))
	if !change.Accepted || change.Revision != 2 || change.Parent != 1 {
		t.Fatalf("unexpected update lineage %+v", change)
	}
	behaviorRequireBytes(t, filepath.Join(root, "config/rules.txt"), second)
	behaviorRequireObject(t, root, behaviorDigest(first), first)
	behaviorRequireObject(t, root, behaviorDigest(second), second)
	declared := behaviorDeclared(t, root)
	if len(declared) != 1 || declared[0].Hash != behaviorDigest(second) {
		t.Fatalf("manifest still accepts %+v", declared)
	}
}

func TestFileRevertNavigatesRecurringDigestsByParent(t *testing.T) {
	root := behaviorWorkspace(t)
	alpha, beta := []byte("alpha\n"), []byte("beta\n")
	path := "config/rules.txt"
	behaviorWrite(t, root, path, alpha, 0o644)
	behaviorRun(t, root, Request{Operation: Track, Paths: []string{path}})
	behaviorWrite(t, root, path, beta, 0o644)
	behaviorRun(t, root, Request{Operation: Update, Paths: []string{path}})
	behaviorWrite(t, root, path, alpha, 0o644)
	behaviorRun(t, root, Request{Operation: Update, Paths: []string{path}})

	events := behaviorHistory(t, root, path)
	if len(events) != 3 {
		t.Fatalf("history holds %d events, want 3", len(events))
	}
	wantRevisions := []uint64{1, 2, 3}
	wantParents := []uint64{0, 1, 2}
	for i, event := range events {
		change := behaviorSoleChange(t, Result{Event: event})
		if change.Revision != wantRevisions[i] || change.Parent != wantParents[i] {
			t.Fatalf("event %d lineage = %+v", i+1, change)
		}
	}

	moved := behaviorSoleChange(t, behaviorRun(t, root, Request{Operation: Revert, Paths: []string{path}}))
	if moved.Revision != 2 || moved.Accepted {
		t.Fatalf("default revert selected %+v, want revision 2", moved)
	}
	behaviorRequireBytes(t, filepath.Join(root, path), beta)
	if declared := behaviorDeclared(t, root); declared[0].Hash != behaviorDigest(beta) {
		t.Fatalf("revert accepted %s, want the parent revision", declared[0].Hash)
	}

	behaviorRun(t, root, Request{Operation: Revert, Paths: []string{path}})
	behaviorRequireBytes(t, filepath.Join(root, path), alpha)
	if declared := behaviorDeclared(t, root); declared[0].Hash != behaviorDigest(alpha) {
		t.Fatalf("second revert accepted %s, want the grandparent revision", declared[0].Hash)
	}
	events = behaviorHistory(t, root, path)
	if len(events) != 5 {
		t.Fatalf("history holds %d events after two reverts, want 5", len(events))
	}
	for _, event := range events[3:] {
		change := behaviorSoleChange(t, Result{Event: event})
		if change.Accepted {
			t.Fatalf("revert %d was recorded as an accepted revision", event.Sequence)
		}
	}
}

func TestFileRevertTargetsSelectRevisionAndSequence(t *testing.T) {
	root := behaviorWorkspace(t)
	alpha, beta := []byte("alpha\n"), []byte("beta\n")
	path := "config/rules.txt"
	behaviorWrite(t, root, path, alpha, 0o644)
	behaviorRun(t, root, Request{Operation: Track, Paths: []string{path}})
	behaviorWrite(t, root, path, beta, 0o644)
	behaviorRun(t, root, Request{Operation: Update, Paths: []string{path}})
	behaviorWrite(t, root, path, alpha, 0o644)
	behaviorRun(t, root, Request{Operation: Update, Paths: []string{path}})

	change := behaviorSoleChange(t, behaviorRun(t, root, Request{
		Operation: Revert,
		Paths:     []string{path},
		Hash:      behaviorDigest(alpha),
	}))
	if change.Revision != 3 {
		t.Fatalf("hash targeting selected revision %d, want 3", change.Revision)
	}
	behaviorRequireBytes(t, filepath.Join(root, path), alpha)

	behaviorRun(t, root, Request{Operation: Revert, Paths: []string{path}, Revision: 2})
	behaviorRequireBytes(t, filepath.Join(root, path), beta)
	if declared := behaviorDeclared(t, root); declared[0].Hash != behaviorDigest(beta) {
		t.Fatalf("sequence revert accepted %s", declared[0].Hash)
	}

	beforeManifest := behaviorRead(t, root, documentpkg.ManifestFilename)
	beforeHistory := behaviorRead(t, root, ".lucy/history/files.jsonl")
	for _, request := range []Request{
		{Operation: Revert, Paths: []string{path}, Hash: behaviorDigest(alpha), Revision: 2},
		{Operation: Revert, Paths: []string{path}, Revision: 99},
		{Operation: Revert, Paths: []string{path}, Hash: behaviorDigest([]byte("never accepted"))},
	} {
		behaviorRunErr(t, root, request)
	}
	behaviorRequireBytes(t, filepath.Join(root, path), beta)
	behaviorRequireBytes(t, filepath.Join(root, documentpkg.ManifestFilename), beforeManifest)
	behaviorRequireBytes(t, filepath.Join(root, ".lucy/history/files.jsonl"), beforeHistory)
}

func TestFileLockAndUnlockControlDriftState(t *testing.T) {
	root := behaviorWorkspace(t)
	source := newBehaviorSource(t, []byte("published rules\n"))
	first := []byte("rules v1\n")
	behaviorWrite(t, root, "config/rules.txt", first, 0o644)
	behaviorRun(t, root, Request{
		Operation:  Track,
		Paths:      []string{"config/rules.txt"},
		RecoverURL: source.url("rules.txt"),
	})

	change := behaviorSoleChange(t, behaviorRun(t, root, Request{
		Operation: Lock,
		Paths:     []string{"config/rules.txt"},
	}))
	if change.After == nil || !change.After.Immutable || change.After.Hash != behaviorDigest(first) ||
		change.After.RecoverURL != source.url("rules.txt") {
		t.Fatalf("lock changed the declaration: %+v", change.After)
	}

	drifted := []byte("drifted rules\n")
	behaviorWrite(t, root, "config/rules.txt", drifted, 0o644)
	status := behaviorStatus(t, root, "config/rules.txt")
	if status.State != "modified" || !status.Immutable {
		t.Fatalf("locked drift is not reported as a modified immutable file: %+v", status)
	}
	if status.Hash != behaviorDigest(first) || status.ActualHash != behaviorDigest(drifted) {
		t.Fatalf("drift hides one of the two digests: %+v", status)
	}

	unlocked := behaviorSoleChange(t, behaviorRun(t, root, Request{
		Operation: Unlock,
		Paths:     []string{"config/rules.txt"},
	}))
	if unlocked.After == nil || unlocked.After.Immutable {
		t.Fatalf("unlock kept the immutable policy: %+v", unlocked.After)
	}
	declared := behaviorDeclared(t, root)
	if len(declared) != 1 || declared[0].Immutable ||
		declared[0].Hash != behaviorDigest(first) ||
		declared[0].RecoverURL != source.url("rules.txt") {
		t.Fatalf("unlock lost the hash or the recovery URL: %+v", declared)
	}
	if status := behaviorStatus(t, root, "config/rules.txt"); status.State != "modified" || status.Immutable {
		t.Fatalf("mutable drift is not reported plainly: %+v", status)
	}
}

func TestFileUntrackKeepsWorkingBytesAndHistory(t *testing.T) {
	root := behaviorWorkspace(t)
	rules := []byte("rules\n")
	behaviorWrite(t, root, "config/rules.txt", rules, 0o644)
	behaviorRun(t, root, Request{Operation: Track, Paths: []string{"config/rules.txt"}})

	behaviorRun(t, root, Request{Operation: Untrack, Paths: []string{"config/rules.txt"}})

	behaviorRequireBytes(t, filepath.Join(root, "config/rules.txt"), rules)
	behaviorRequireObject(t, root, behaviorDigest(rules), rules)
	if declared := behaviorDeclared(t, root); len(declared) != 0 {
		t.Fatalf("untrack left declarations %+v", declared)
	}
	events := behaviorHistory(t, root, "config/rules.txt")
	if len(events) != 2 || events[0].Operation != Track || events[1].Operation != Untrack {
		t.Fatalf("history of the untracked path = %+v", events)
	}
}

func TestFileInspectionCreatesNoState(t *testing.T) {
	root := behaviorWorkspace(t)
	if statuses, err := Inspect(root, behaviorDeclared(t, root)); err != nil {
		t.Fatalf("Inspect() error = %v", err)
	} else if len(statuses) != 0 {
		t.Fatalf("read-only inspection invented %d statuses", len(statuses))
	}
	if events := behaviorHistory(t, root, ""); len(events) != 0 {
		t.Fatalf("read-only history invented %d events", len(events))
	}
	behaviorRequireAbsent(t, root, ".lucy")
}

func TestFileLockOnlyWorkspaceServesInspection(t *testing.T) {
	root := t.TempDir()
	core := []byte("installed core bytes\n")
	rules := []byte("rules v1\n")
	behaviorWrite(t, root, "server/server.jar", core, 0o644)
	behaviorWrite(t, root, "config/rules.txt", rules, 0o644)
	lock := &documentpkg.Lock{
		SchemaVersion:       documentpkg.LockSchemaVersion,
		ManifestFingerprint: behaviorDigest([]byte("a manifest that is gone")),
		Server: documentpkg.LockedServer{
			Minecraft:   "1.21.4",
			Directory:   "server",
			Product:     serverpkg.ProductMinecraft,
			CoreVersion: "1.21.4",
			Artifact: catalog.Artifact{
				Provider: catalog.ProviderMojang,
				Version:  "1.21.4",
				Filename: "server.jar",
				URL:      "https://example.invalid/server.jar",
				Hashes:   map[string]string{"sha256": behaviorHex(core)},
			},
			Java: documentpkg.LockedJava{Advisory: 21},
		},
		Packages: []documentpkg.Package{},
		Files:    []documentpkg.TrackedFile{{Path: "config/rules.txt", Hash: behaviorDigest(rules)}},
	}
	if err := documentpkg.LockFile(root).Write(lock); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	behaviorRequireAbsent(t, root, documentpkg.ManifestFilename)

	status := behaviorStatus(t, root, "config/rules.txt")
	if status.State != "matching" || status.Hash != behaviorDigest(rules) {
		t.Fatalf("the lock-only workspace does not replay the declaration: %+v", status)
	}
	behaviorRequireAbsent(t, root, ".lucy")
	if events := behaviorHistory(t, root, ""); len(events) != 0 {
		t.Fatalf("lock-only history invented events: %+v", events)
	}

	drifted := []byte("drifted rules\n")
	behaviorWrite(t, root, "config/rules.txt", drifted, 0o644)
	if status := behaviorStatus(t, root, "config/rules.txt"); status.State != "modified" ||
		status.ActualHash != behaviorDigest(drifted) {
		t.Fatalf("the lock-only workspace lost the drift: %+v", status)
	}
}

func TestFileTrackNoHashTracksPresenceOnly(t *testing.T) {
	root := behaviorWorkspace(t)
	marker := []byte("marker\n")
	behaviorWrite(t, root, "config/present.bin", marker, 0o644)

	change := behaviorSoleChange(t, behaviorRun(t, root, Request{
		Operation: Track,
		Paths:     []string{"config/present.bin"},
		NoHash:    true,
	}))
	if change.After == nil || change.After.Hash != "" {
		t.Fatalf("presence-only tracking recorded an accepted hash: %+v", change.After)
	}
	status := behaviorStatus(t, root, "config/present.bin")
	if status.State != "presence-only" || status.Hash != "" {
		t.Fatalf("unexpected presence-only status %+v", status)
	}
	behaviorRunErr(t, root, Request{Operation: Recover, Paths: []string{"config/present.bin"}})
	behaviorRequireBytes(t, filepath.Join(root, "config/present.bin"), marker)

	replaced := []byte("marker v2\n")
	behaviorWrite(t, root, "config/present.bin", replaced, 0o644)
	behaviorRun(t, root, Request{Operation: Update, Paths: []string{"config/present.bin"}})
	status = behaviorStatus(t, root, "config/present.bin")
	if status.State != "matching" || status.Hash != behaviorDigest(replaced) {
		t.Fatalf("update did not accept bytes for a previously presence-only file: %+v", status)
	}
}

func TestFileRecoverPrefersLocalObjectBeforeSource(t *testing.T) {
	root := behaviorWorkspace(t)
	source := newBehaviorSource(t, []byte("published rules\n"))
	rules := []byte("accepted rules\n")
	behaviorWrite(t, root, "config/rules.txt", rules, 0o644)
	behaviorRun(t, root, Request{
		Operation:  Track,
		Paths:      []string{"config/rules.txt"},
		RecoverURL: source.url("rules.txt"),
	})
	drifted := []byte("drifted rules\n")
	behaviorWrite(t, root, "config/rules.txt", drifted, 0o644)

	change := behaviorSoleChange(t, behaviorRun(t, root, Request{
		Operation: Recover,
		Paths:     []string{"config/rules.txt"},
	}))
	behaviorRequireBytes(t, filepath.Join(root, "config/rules.txt"), rules)
	if declared := behaviorDeclared(t, root); declared[0].Hash != behaviorDigest(rules) {
		t.Fatalf("recover moved the accepted hash to %s", declared[0].Hash)
	}
	if hits := source.hits(); hits != 0 {
		t.Fatalf("recover fetched the source %d times although the local object answered", hits)
	}
	if change.BackupHash != behaviorDigest(drifted) {
		t.Fatalf("recovery lost the displaced working bytes: %+v", change)
	}
	behaviorRequireObject(t, root, behaviorDigest(drifted), drifted)
}

func TestFileRecoverFetchesAcceptedBytesFromSource(t *testing.T) {
	root := behaviorWorkspace(t)
	rules := []byte("accepted rules\n")
	source := newBehaviorSource(t, rules)
	behaviorWrite(t, root, "config/rules.txt", rules, 0o644)
	behaviorRun(t, root, Request{
		Operation:  Track,
		Paths:      []string{"config/rules.txt"},
		RecoverURL: source.url("rules.txt"),
	})
	behaviorDropObject(t, root, behaviorDigest(rules))
	if err := os.Remove(filepath.Join(root, "config/rules.txt")); err != nil {
		t.Fatalf("remove the working file: %v", err)
	}
	behaviorRequireAbsent(t, root, "config/rules.txt")

	behaviorRun(t, root, Request{Operation: Recover, Paths: []string{"config/rules.txt"}})

	behaviorRequireBytes(t, filepath.Join(root, "config/rules.txt"), rules)
	behaviorRequireObject(t, root, behaviorDigest(rules), rules)
	if hits := source.hits(); hits != 1 {
		t.Fatalf("recover fetched the source %d times, want one", hits)
	}
	if status := behaviorStatus(t, root, "config/rules.txt"); status.State != "matching" {
		t.Fatalf("recovered file is not the accepted revision: %+v", status)
	}
}

func TestFileRecoverKeepsMismatchedTargetAndDeclaration(t *testing.T) {
	root := behaviorWorkspace(t)
	rules := []byte("accepted rules\n")
	source := newBehaviorSource(t, []byte("published rules\n"))
	behaviorWrite(t, root, "config/rules.txt", rules, 0o644)
	behaviorRun(t, root, Request{
		Operation:  Track,
		Paths:      []string{"config/rules.txt"},
		RecoverURL: source.url("rules.txt"),
	})
	drifted := []byte("drifted rules\n")
	behaviorWrite(t, root, "config/rules.txt", drifted, 0o644)
	behaviorDropObject(t, root, behaviorDigest(rules))
	source.publish([]byte("rotated upstream rules\n"))
	beforeHistory := behaviorRead(t, root, ".lucy/history/files.jsonl")

	behaviorRunErr(t, root, Request{Operation: Recover, Paths: []string{"config/rules.txt"}})

	behaviorRequireBytes(t, filepath.Join(root, "config/rules.txt"), drifted)
	if declared := behaviorDeclared(t, root); declared[0].Hash != behaviorDigest(rules) {
		t.Fatalf("a failed recovery moved the accepted hash to %s", declared[0].Hash)
	}
	behaviorRequireBytes(t, filepath.Join(root, ".lucy/history/files.jsonl"), beforeHistory)
	if hits := source.hits(); hits != 1 {
		t.Fatalf("recovery fetched the source %d times, want exactly one attempt", hits)
	}
}

func TestFileUpdateFromSourceAcceptsBytesAndKeepsBackup(t *testing.T) {
	root := behaviorWorkspace(t)
	first, second := []byte("published rules 1\n"), []byte("published rules 2\n")
	source := newBehaviorSource(t, first)
	working := []byte("working rules 1\n")
	behaviorWrite(t, root, "config/rules.txt", working, 0o644)
	behaviorRun(t, root, Request{
		Operation:  Track,
		Paths:      []string{"config/rules.txt"},
		RecoverURL: source.url("rules.txt"),
	})
	source.publish(second)

	change := behaviorSoleChange(t, behaviorRun(t, root, Request{
		Operation:  Update,
		Paths:      []string{"config/rules.txt"},
		FromSource: true,
	}))
	if !change.Accepted || change.Revision != 2 || change.Parent != 1 {
		t.Fatalf("unexpected source update lineage %+v", change)
	}
	if change.After == nil || change.After.Hash != behaviorDigest(second) {
		t.Fatalf("source update did not accept the published bytes: %+v", change.After)
	}
	if change.BackupHash != behaviorDigest(working) {
		t.Fatalf("source update lost the displaced working bytes: %+v", change)
	}
	behaviorRequireBytes(t, filepath.Join(root, "config/rules.txt"), second)
	behaviorRequireObject(t, root, behaviorDigest(working), working)
	if declared := behaviorDeclared(t, root); declared[0].Hash != behaviorDigest(second) ||
		declared[0].RecoverURL != source.url("rules.txt") {
		t.Fatalf("source update rewrote the declaration: %+v", declared)
	}

	third := []byte("published rules 3\n")
	source.publish(third)
	fresh := behaviorSoleChange(t, behaviorRun(t, root, Request{
		Operation:  Update,
		Paths:      []string{"config/rules.txt"},
		FromSource: true,
	}))
	if fresh.After == nil || fresh.After.Hash != behaviorDigest(third) {
		t.Fatalf("stale bytes kept the source update back: %+v", fresh)
	}
	if hits := source.hits(); hits != 2 {
		t.Fatalf("source update fetched the source %d times, want two fresh fetches", hits)
	}
}

func TestFileUpdateAllFailsAtomicallyThenSucceeds(t *testing.T) {
	root := behaviorWorkspace(t)
	oldA, newA, oldB := []byte("rules a v1\n"), []byte("rules a v2\n"), []byte("rules b v1\n")
	behaviorWrite(t, root, "config/a.txt", oldA, 0o644)
	behaviorWrite(t, root, "config/b.txt", oldB, 0o644)
	behaviorRun(t, root, Request{Operation: Track, Paths: []string{"config/a.txt"}})
	behaviorRun(t, root, Request{Operation: Track, Paths: []string{"config/b.txt"}})
	behaviorWrite(t, root, "config/a.txt", newA, 0o644)
	if err := os.Remove(filepath.Join(root, "config/b.txt")); err != nil {
		t.Fatalf("remove tracked file: %v", err)
	}

	manifestBytes := behaviorRead(t, root, documentpkg.ManifestFilename)
	behaviorRunErr(t, root, Request{Operation: Update, All: true})
	behaviorRequireBytes(t, filepath.Join(root, documentpkg.ManifestFilename), manifestBytes)
	behaviorRequireBytes(t, filepath.Join(root, "config/a.txt"), newA)
	if events := behaviorHistory(t, root, ""); len(events) != 2 {
		t.Fatalf("a failed bulk update left %d history events, want 2", len(events))
	}
	if status := behaviorStatus(t, root, "config/a.txt"); status.State != "modified" {
		t.Fatalf("the failed bulk update accepted the other file anyway: %+v", status)
	}

	behaviorWrite(t, root, "config/b.txt", oldB, 0o644)
	result := behaviorRun(t, root, Request{Operation: Update, All: true})
	if len(result.Event.Changes) != 2 {
		t.Fatalf("bulk update returned %d changes, want 2", len(result.Event.Changes))
	}
	for _, change := range result.Event.Changes {
		if !change.Accepted || change.Revision != 3 {
			t.Fatalf("bulk update change is not an accepted revision: %+v", change)
		}
	}
	hashes := map[string]string{}
	for _, file := range behaviorDeclared(t, root) {
		hashes[file.Path] = file.Hash
	}
	if hashes["config/a.txt"] != behaviorDigest(newA) || hashes["config/b.txt"] != behaviorDigest(oldB) {
		t.Fatalf("bulk update accepted the wrong bytes: %+v", hashes)
	}
}

func TestFileRecoverAllSkipsPresenceOnlyEntries(t *testing.T) {
	root := behaviorWorkspace(t)
	rules := []byte("accepted rules\n")
	behaviorWrite(t, root, "config/a.txt", rules, 0o644)
	behaviorWrite(t, root, "config/present.bin", []byte("marker\n"), 0o644)
	behaviorRun(t, root, Request{Operation: Track, Paths: []string{"config/a.txt"}})
	behaviorRun(t, root, Request{
		Operation: Track,
		Paths:     []string{"config/present.bin"},
		NoHash:    true,
	})
	behaviorWrite(t, root, "config/a.txt", []byte("drifted\n"), 0o644)

	result := behaviorRun(t, root, Request{Operation: Recover, All: true})
	if len(result.Skipped) != 1 || result.Skipped[0] != "config/present.bin" {
		t.Fatalf("bulk recovery skipped %v", result.Skipped)
	}
	if len(result.Event.Changes) != 1 || result.Event.Changes[0].Path != "config/a.txt" {
		t.Fatalf("bulk recovery restored %+v", result.Event.Changes)
	}
	behaviorRequireBytes(t, filepath.Join(root, "config/a.txt"), rules)
	behaviorRequireBytes(t, filepath.Join(root, "config/present.bin"), []byte("marker\n"))
	if status := behaviorStatus(t, root, "config/a.txt"); status.Hash != behaviorDigest(rules) ||
		status.State != "matching" {
		t.Fatalf("bulk recovery changed the accepted digest: %+v", status)
	}
}

func TestFileUpdateAllFromSourceSkipsEntriesWithoutURL(t *testing.T) {
	root := behaviorWorkspace(t)
	published := []byte("published rules\n")
	source := newBehaviorSource(t, published)
	behaviorWrite(t, root, "config/a.txt", []byte("working a\n"), 0o644)
	behaviorWrite(t, root, "config/b.txt", []byte("working b\n"), 0o644)
	behaviorRun(t, root, Request{
		Operation:  Track,
		Paths:      []string{"config/a.txt"},
		RecoverURL: source.url("a.txt"),
	})
	behaviorRun(t, root, Request{Operation: Track, Paths: []string{"config/b.txt"}})
	source.publish([]byte("fresh published rules\n"))

	result := behaviorRun(t, root, Request{
		Operation:  Update,
		All:        true,
		FromSource: true,
	})
	if len(result.Skipped) != 1 || result.Skipped[0] != "config/b.txt" {
		t.Fatalf("bulk source update skipped %v", result.Skipped)
	}
	if len(result.Event.Changes) != 1 || result.Event.Changes[0].Path != "config/a.txt" {
		t.Fatalf("bulk source update accepted %+v", result.Event.Changes)
	}
	behaviorRequireBytes(t, filepath.Join(root, "config/a.txt"), []byte("fresh published rules\n"))
	behaviorRequireBytes(t, filepath.Join(root, "config/b.txt"), []byte("working b\n"))
	if status := behaviorStatus(t, root, "config/a.txt"); status.State != "matching" ||
		status.Hash != behaviorDigest([]byte("fresh published rules\n")) {
		t.Fatalf("bulk source update did not accept the replacement bytes: %+v", status)
	}
	if declared := behaviorDeclared(t, root); len(declared) != 2 {
		t.Fatalf("manifest holds %d declarations after the skipped bulk update", len(declared))
	}
}

func TestFileTrackRejectsForbiddenAndNonRegularPaths(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := behaviorWriteFile(outside, []byte("outside the workspace\n"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	for _, test := range []struct{ name, path string }{
		{"absolute", outside},
		{"backslash", `config\rules.txt`},
		{"private state", ".lucy/state.txt"},
		{"git state", ".git/config"},
		{"manifest", documentpkg.ManifestFilename},
		{"lock", documentpkg.LockFilename},
		{"manifest descendant", documentpkg.ManifestFilename + "/config.txt"},
		{"lock descendant", documentpkg.LockFilename + "/config.txt"},
		{"root directory", "."},
		{"directory", "server"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := behaviorWorkspace(t)
			behaviorWrite(t, root, "config/retained.txt", []byte("retained\n"), 0o644)
			if err := os.MkdirAll(filepath.Join(root, "server"), 0o755); err != nil {
				t.Fatalf("create server directory: %v", err)
			}
			before := behaviorRead(t, root, documentpkg.ManifestFilename)
			behaviorRunErr(t, root, Request{Operation: Track, Paths: []string{test.path}})
			behaviorRequireBytes(t, filepath.Join(root, documentpkg.ManifestFilename), before)
			behaviorRequireBytes(t, filepath.Join(root, "config/retained.txt"), []byte("retained\n"))
			behaviorRequireAbsent(t, root, ".lucy/history/files.jsonl")
		})
	}
}

func TestFileTrackRejectsTraversalToExistingFile(t *testing.T) {
	root := behaviorWorkspace(t)
	outside := []byte("outside the workspace\n")
	sibling := filepath.Join(filepath.Dir(root), "outside.txt")
	if err := behaviorWriteFile(sibling, outside, 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	before := behaviorRead(t, root, documentpkg.ManifestFilename)

	behaviorRunErr(t, root, Request{Operation: Track, Paths: []string{"../outside.txt"}})

	behaviorRequireBytes(t, sibling, outside)
	behaviorRequireBytes(t, filepath.Join(root, documentpkg.ManifestFilename), before)
	behaviorRequireAbsent(t, root, ".lucy/history/files.jsonl")
}

func TestFileTrackRejectsSymlinkedPaths(t *testing.T) {
	root := behaviorWorkspace(t)
	outside := filepath.Join(t.TempDir(), "outside")
	inside := []byte("outside\n")
	if err := behaviorWriteFile(filepath.Join(outside, "inside.txt"), inside, 0o644); err != nil {
		t.Fatalf("write symlink target: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "config"), 0o755); err != nil {
		t.Fatalf("create config directory: %v", err)
	}
	if err := os.Symlink(filepath.Join(outside, "inside.txt"), filepath.Join(root, "config/linked.txt")); err != nil {
		t.Fatalf("symlink file: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "outside")); err != nil {
		t.Fatalf("symlink directory: %v", err)
	}
	behaviorWrite(t, root, "real/inside.txt", []byte("inside\n"), 0o644)
	if err := os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "internal")); err != nil {
		t.Fatalf("symlink internal directory: %v", err)
	}

	for _, path := range []string{"config/linked.txt", "outside/inside.txt", "internal/inside.txt"} {
		behaviorRunErr(t, root, Request{Operation: Track, Paths: []string{path}})
	}
	behaviorRequireBytes(t, filepath.Join(root, "real/inside.txt"), []byte("inside\n"))
	behaviorRequireBytes(t, filepath.Join(root, "outside/inside.txt"), inside)
	if declared := behaviorDeclared(t, root); len(declared) != 0 {
		t.Fatalf("symlinked paths left %d declarations behind", len(declared))
	}
}

func TestFileInspectReportsSymlinkedDeclarationAsError(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	inside := []byte("outside\n")
	if err := behaviorWriteFile(filepath.Join(outside, "inside.txt"), inside, 0o644); err != nil {
		t.Fatalf("write symlink target: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "outside")); err != nil {
		t.Fatalf("symlink directory: %v", err)
	}
	declared := behaviorManifest()
	declared.Files = []documentpkg.TrackedFile{{Path: "outside/inside.txt", Hash: behaviorDigest(inside)}}
	behaviorWriteManifest(t, root, declared)

	status := behaviorStatus(t, root, "outside/inside.txt")
	if status.State != "error" {
		t.Fatalf("symlinked declaration is not an error state: %+v", status)
	}
}

func TestFileNormalizationRejectsInvalidDeclarations(t *testing.T) {
	hash := behaviorDigest([]byte("shared\n"))
	for _, test := range []struct {
		name  string
		files []documentpkg.TrackedFile
	}{
		{
			name: "duplicate normalized",
			files: []documentpkg.TrackedFile{
				{Path: "./config/a.txt", Hash: hash},
				{Path: "config/a.txt", Hash: hash},
			},
		},
		{
			name: "case-insensitive duplicate",
			files: []documentpkg.TrackedFile{
				{Path: "config/A.txt", Hash: hash},
				{Path: "config/a.txt", Hash: hash},
			},
		},
		{
			name: "immutable without hash",
			files: []documentpkg.TrackedFile{
				{Path: "config/rules.txt", Immutable: true},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			files := append([]documentpkg.TrackedFile(nil), test.files...)
			if err := documentpkg.NormalizeTrackedFiles(files); err == nil {
				t.Fatalf("NormalizeTrackedFiles(%+v) accepted invalid declarations", test.files)
			}
		})
	}
}

func TestFileTrackRejectsConflictingChoices(t *testing.T) {
	root := behaviorWorkspace(t)
	path := "config/rules.txt"
	rules := []byte("rules\n")
	behaviorWrite(t, root, path, rules, 0o644)
	beforeManifest := behaviorRead(t, root, documentpkg.ManifestFilename)

	for _, request := range []Request{
		{Operation: Track, Paths: []string{path}, NoHash: true, Immutable: true},
		{Operation: Track, Paths: []string{path}, NoHash: true, RecoverURL: "http://127.0.0.1:9/rules.txt"},
		{Operation: Track, Paths: []string{path}, RecoverURL: "not-a-url"},
		{Operation: Track, Paths: []string{path}, RecoverURL: "ftp://127.0.0.1/rules.txt"},
		{Operation: Update, Paths: []string{path}, FromSource: true},
		{Operation: Update, All: true, Paths: []string{path}},
		{Operation: Recover, All: true, Paths: []string{path}},
		{Operation: Update},
	} {
		behaviorRunErr(t, root, request)
	}
	behaviorRequireBytes(t, filepath.Join(root, documentpkg.ManifestFilename), beforeManifest)
	behaviorRequireBytes(t, filepath.Join(root, path), rules)
	behaviorRequireAbsent(t, root, ".lucy/history/files.jsonl")

	behaviorRun(t, root, Request{Operation: Track, Paths: []string{path}})
	beforeManifest = behaviorRead(t, root, documentpkg.ManifestFilename)
	beforeHistory := behaviorRead(t, root, ".lucy/history/files.jsonl")
	behaviorRunErr(t, root, Request{Operation: Update, Paths: []string{path}, FromSource: true})
	behaviorRunErr(t, root, Request{Operation: Update, All: true, Paths: []string{path}})
	behaviorRunErr(t, root, Request{Operation: Recover, All: true, Paths: []string{path}})
	behaviorRunErr(t, root, Request{Operation: Update})
	behaviorRequireBytes(t, filepath.Join(root, documentpkg.ManifestFilename), beforeManifest)
	behaviorRequireBytes(t, filepath.Join(root, ".lucy/history/files.jsonl"), beforeHistory)
	if declared := behaviorDeclared(t, root); len(declared) != 1 || declared[0].Hash != behaviorDigest(rules) {
		t.Fatalf("rejected choices disturbed the tracking declaration: %+v", declared)
	}
}

func TestFileTrackRejectsManagedOutputCollisions(t *testing.T) {
	t.Run("generated receipt output", func(t *testing.T) {
		root := behaviorWorkspace(t)
		notes := []byte("generated note\n")
		behaviorWrite(t, root, "generated/notes.txt", notes, 0o644)
		receipt := struct {
			Version int `yaml:"version"`
			Entries []struct {
				Path   string `yaml:"path"`
				SHA256 string `yaml:"sha256"`
				Mode   uint32 `yaml:"mode"`
			} `yaml:"entries"`
		}{Version: 1, Entries: []struct {
			Path   string `yaml:"path"`
			SHA256 string `yaml:"sha256"`
			Mode   uint32 `yaml:"mode"`
		}{{Path: "generated/notes.txt", SHA256: behaviorHex(notes), Mode: 0o644}}}
		data, err := yaml.Marshal(receipt)
		if err != nil {
			t.Fatalf("marshal receipt: %v", err)
		}
		behaviorWrite(t, root, ".lucy/install.yaml", data, 0o600)

		behaviorRunErr(t, root, Request{Operation: Track, Paths: []string{"generated/notes.txt"}})
		behaviorRequireBytes(t, filepath.Join(root, "generated/notes.txt"), notes)
		if declared := behaviorDeclared(t, root); len(declared) != 0 {
			t.Fatalf("the collision still published a declaration: %+v", declared)
		}
	})

	t.Run("managed core", func(t *testing.T) {
		root := behaviorWorkspace(t)
		core := []byte("core bytes\n")
		behaviorWrite(t, root, "server/server.jar", core, 0o644)
		lock := behaviorLock(t, root, core)
		if err := documentpkg.LockFile(root).Write(lock); err != nil {
			t.Fatalf("write lock: %v", err)
		}

		behaviorRunErr(t, root, Request{Operation: Track, Paths: []string{"server/server.jar"}})
		behaviorRequireBytes(t, filepath.Join(root, "server/server.jar"), core)
		if declared := behaviorDeclared(t, root); len(declared) != 0 {
			t.Fatalf("the collision still published a declaration: %+v", declared)
		}
	})

	t.Run("locked package output", func(t *testing.T) {
		root := behaviorWorkspace(t)
		jar := []byte("package jar bytes\n")
		behaviorWrite(t, root, "server/mods/sodium.jar", jar, 0o644)
		lock := behaviorLock(t, root, []byte("core bytes\n"))
		lock.Packages = []documentpkg.Package{{
			Environment: serverpkg.EnvironmentServer,
			Ecosystem:   catalog.EcosystemFabric,
			Artifact: catalog.Artifact{
				Provider: catalog.ProviderModrinth,
				Version:  "0.6.0",
				Filename: "sodium.jar",
				URL:      "https://example.invalid/sodium.jar",
				Hashes:   map[string]string{"sha256": behaviorHex(jar)},
			},
		}}
		if err := documentpkg.LockFile(root).Write(lock); err != nil {
			t.Fatalf("write lock: %v", err)
		}

		behaviorRunErr(t, root, Request{Operation: Track, Paths: []string{"server/mods/sodium.jar"}})
		behaviorRequireBytes(t, filepath.Join(root, "server/mods/sodium.jar"), jar)
	})

	t.Run("mcdr wrapper", func(t *testing.T) {
		root := behaviorWorkspace(t)
		lock := behaviorLock(t, root, []byte("core bytes\n"))
		lock.MCDR = &documentpkg.LockedMCDR{Directory: ".", Version: "2.14.0"}
		if err := documentpkg.LockFile(root).Write(lock); err != nil {
			t.Fatalf("write lock: %v", err)
		}
		behaviorWrite(t, root, "run-mcdr.sh", []byte("#!/bin/sh\n"), 0o755)
		behaviorRunErr(t, root, Request{Operation: Track, Paths: []string{"run-mcdr.sh"}})
		if declared := behaviorDeclared(t, root); len(declared) != 0 {
			t.Fatalf("the wrapper collision changed the declaration set: %+v", declared)
		}
	})
}

func TestDisabledPackageLeavesFolderAvailable(t *testing.T) {
	root := behaviorWorkspace(t)
	lock := behaviorLock(t, root, []byte("core bytes\n"))
	lock.Packages = []documentpkg.Package{{
		Environment: serverpkg.EnvironmentServer,
		Ecosystem:   catalog.EcosystemFabric,
		Artifact: catalog.Artifact{
			Provider:  catalog.ProviderModrinth,
			ProjectID: "sodium",
			Version:   "0.6.0",
			Filename:  "sodium.jar",
			URL:       "https://example.invalid/sodium.jar",
			Hashes:    map[string]string{"sha256": behaviorHex([]byte("disabled sodium"))},
		},
		Requirements: []documentpkg.RequirementBinding{{
			Reference: "modrinth:sodium",
			Selector:  "stable",
			Enabled:   false,
		}},
	}}
	if err := documentpkg.LockFile(root).Write(lock); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	behaviorWrite(t, root, "server/mods/notes.txt", []byte("handwritten note\n"), 0o644)

	behaviorRun(t, root, Request{Operation: Track, Paths: []string{"server/mods/notes.txt"}})

	status := behaviorStatus(t, root, "server/mods/notes.txt")
	if status.State != "matching" {
		t.Fatalf("the disabled package blocked a folder neighbour: %+v", status)
	}
	declared := behaviorDeclared(t, root)
	if len(declared) != 1 || declared[0].Path != "server/mods/notes.txt" {
		t.Fatalf("unexpected declaration set: %+v", declared)
	}
}

func TestFileCommandsPreserveDocumentModes(t *testing.T) {
	root := behaviorWorkspace(t)
	rules := []byte("rules\n")
	behaviorWrite(t, root, "config/rules.txt", rules, 0o644)
	if err := documentpkg.LockFile(root).Write(behaviorLock(t, root, []byte("core bytes\n"))); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	behaviorRun(t, root, Request{Operation: Track, Paths: []string{"config/rules.txt"}})
	modes := make(map[string]os.FileMode)
	for _, name := range []string{documentpkg.ManifestFilename, documentpkg.LockFilename} {
		if err := os.Chmod(filepath.Join(root, name), 0o640); err != nil {
			t.Fatalf("chmod %s: %v", name, err)
		}
		info, err := os.Stat(filepath.Join(root, name))
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		modes[name] = info.Mode().Perm()
		if runtime.GOOS != "windows" && modes[name] != 0o640 {
			t.Fatalf("%s mode = %v, want 0640", name, modes[name])
		}
	}
	behaviorWrite(t, root, "config/extra.txt", []byte("extra note\n"), 0o644)
	behaviorRun(t, root, Request{Operation: Track, Paths: []string{"config/extra.txt"}})
	for _, name := range []string{documentpkg.ManifestFilename, documentpkg.LockFilename} {
		info, err := os.Stat(filepath.Join(root, name))
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		if info.Mode().Perm() != modes[name] {
			t.Fatalf("%s mode = %v, want %v", name, info.Mode().Perm(), modes[name])
		}
	}
}

// behaviorLock returns a current lock over core bytes. Callers add packages or
// MCDR state before writing it.
func behaviorLock(t *testing.T, root string, core []byte) *documentpkg.Lock {
	t.Helper()
	document, err := documentpkg.ManifestFile(root).Read()
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	fingerprint, err := document.Fingerprint()
	if err != nil {
		t.Fatalf("fingerprint manifest: %v", err)
	}
	return &documentpkg.Lock{
		SchemaVersion:       documentpkg.LockSchemaVersion,
		ManifestFingerprint: fingerprint,
		Server: documentpkg.LockedServer{
			Minecraft:   "1.21.4",
			Directory:   "server",
			Product:     serverpkg.ProductMinecraft,
			CoreVersion: "1.21.4",
			Artifact: catalog.Artifact{
				Provider: catalog.ProviderMojang,
				Version:  "1.21.4",
				Filename: "server.jar",
				URL:      "https://example.invalid/server.jar",
				Hashes:   map[string]string{"sha256": behaviorHex(core)},
			},
			Java: documentpkg.LockedJava{Advisory: 21},
		},
		Packages: []documentpkg.Package{},
	}
}

// behaviorSource serves recovery bytes and counts requests, so tests prove
// when the service fetched a URL instead of using local storage.
type behaviorSource struct {
	server   *httptest.Server
	mu       sync.Mutex
	content  []byte
	requests int
}

func newBehaviorSource(t *testing.T, initial []byte) *behaviorSource {
	t.Helper()
	source := &behaviorSource{content: initial}
	source.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		source.mu.Lock()
		source.requests++
		content := source.content
		source.mu.Unlock()
		writer.Header().Set("Content-Type", "application/octet-stream")
		if _, err := writer.Write(content); err != nil {
			t.Errorf("serve recovery bytes: %v", err)
		}
	}))
	t.Cleanup(source.server.Close)
	return source
}

func (s *behaviorSource) url(name string) string {
	return s.server.URL + "/" + name
}

func (s *behaviorSource) publish(content []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.content = content
}

func (s *behaviorSource) hits() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests
}

func TestFileRootRelativeMCDRWrapperLayout(t *testing.T) {
	root := behaviorWorkspace(t)
	lock := behaviorLock(t, root, []byte("core bytes\n"))
	lock.MCDR = &documentpkg.LockedMCDR{Directory: ".", Version: "2.14.0"}
	if err := documentpkg.LockFile(root).Write(lock); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	notes := []byte("wrapper note\n")
	behaviorWrite(t, root, "wrapper-notes.txt", notes, 0o644)

	behaviorRun(t, root, Request{Operation: Track, Paths: []string{"wrapper-notes.txt"}})

	status := behaviorStatus(t, root, "wrapper-notes.txt")
	if status.State != "matching" {
		t.Fatalf("the root-relative wrapper layout rejected a workspace file: %+v", status)
	}
	declared := behaviorDeclared(t, root)
	if len(declared) != 1 || declared[0].Path != "wrapper-notes.txt" {
		t.Fatalf("unexpected declaration set: %+v", declared)
	}
	behaviorWrite(t, root, "run-mcdr.sh", []byte("#!/bin/sh\n"), 0o755)
	behaviorRunErr(t, root, Request{Operation: Track, Paths: []string{"run-mcdr.sh"}})
	if declared := behaviorDeclared(t, root); len(declared) != 1 {
		t.Fatalf("the wrapper collision changed the declaration set: %+v", declared)
	}
}
