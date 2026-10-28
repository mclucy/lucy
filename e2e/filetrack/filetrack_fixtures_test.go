//go:build integration

package filetrack_test

import (
	"os"
	"testing"

	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/server"
)

func baseManifest(t *testing.T) *document.Manifest {
	t.Helper()
	return &document.Manifest{
		SchemaVersion: document.ManifestSchemaVersion,
		Server: document.ManifestServer{
			Minecraft: "1.21.4",
			Directory: "server",
			Core:      document.ManifestCore{Product: server.ProductMinecraft},
		},
	}
}

func TestFileTrackSnapshotsBytesAndRevision(t *testing.T) {
	w := newWorkspace(t)
	w.writeManifest(t, baseManifest(t))
	rules := []byte("keep the world border at 3000\n")
	w.write(t, "config/rules.txt", rules, 0o644)

	w.lucyOK(t, "file", "track", "config/rules.txt")
	w.requireCASObject(t, digestHex(rules), rules)

	doc := w.readManifest(t)
	if len(doc.Files) != 1 {
		t.Fatalf("manifest declares %d files, want 1", len(doc.Files))
	}
	declared := doc.Files[0]
	if declared.Path != "config/rules.txt" || declared.Hash != acceptedHash(rules) ||
		declared.Immutable || declared.RecoverURL != "" {
		t.Errorf("unexpected declaration %+v", declared)
	}

	status := w.statusOf(t, "config/rules.txt")
	if status.State != "matching" || status.Hash != acceptedHash(rules) || status.ActualHash != acceptedHash(rules) {
		t.Errorf("unexpected live status %+v", status)
	}
	w.lucyOK(t, "file", "list")

	events := w.lucyHistory(t, "")
	if len(events) != 1 {
		t.Fatalf("history holds %d events, want 1", len(events))
	}
	if events[0].Sequence != 1 || events[0].Operation != "track" {
		t.Errorf("unexpected history event %+v", events[0])
	}
	change := requireChange(t, events[0], "config/rules.txt")
	if !change.Accepted || change.Revision != 1 || change.Parent != 0 {
		t.Errorf("unexpected accepted revision %+v", change)
	}
	if change.After == nil || change.After.Hash != acceptedHash(rules) {
		t.Errorf("unexpected accepted declaration %+v", change.After)
	}

	persisted := w.persistedHistory(t)
	if len(persisted) != 1 || persisted[0].Sequence != 1 {
		t.Errorf("persisted history does not hold one sequenced event: %+v", persisted)
	}
}

func TestFileTrackRejectsAlreadyTrackedPath(t *testing.T) {
	w := newWorkspace(t)
	w.writeManifest(t, baseManifest(t))
	rules := []byte("rules\n")
	w.write(t, "config/rules.txt", rules, 0o644)
	w.lucyOK(t, "file", "track", "config/rules.txt")

	manifestBytes := w.read(t, document.ManifestFilename)
	w.lucyFail(t, "file", "track", "config/rules.txt")
	w.requireBytes(t, document.ManifestFilename, manifestBytes)
	if events := w.persistedHistory(t); len(events) != 1 {
		t.Errorf("rejected track left %d history events, want 1", len(events))
	}
}

func TestFileUpdateAcceptsFreshRevisionWithParent(t *testing.T) {
	w := newWorkspace(t)
	w.writeManifest(t, baseManifest(t))
	first := []byte("rules v1\n")
	second := []byte("rules v2\n")
	w.write(t, "config/rules.txt", first, 0o644)
	w.lucyOK(t, "file", "track", "config/rules.txt")
	w.write(t, "config/rules.txt", second, 0o644)

	w.lucyOK(t, "file", "update", "config/rules.txt")

	w.requireBytes(t, "config/rules.txt", second)
	w.requireCASObject(t, digestHex(first), first)
	w.requireCASObject(t, digestHex(second), second)
	if declared := w.readManifest(t).Files[0]; declared.Hash != acceptedHash(second) {
		t.Errorf("manifest still accepts %s", declared.Hash)
	}
	events := w.lucyHistory(t, "")
	if len(events) != 2 || events[1].Sequence != 2 {
		t.Fatalf("history does not record the update: %+v", events)
	}
	moved := requireChange(t, events[1], "config/rules.txt")
	if !moved.Accepted || moved.Revision != 2 || moved.Parent != 1 {
		t.Errorf("unexpected update revision lineage %+v", moved)
	}
	if moved.After == nil || moved.After.Hash != acceptedHash(second) {
		t.Errorf("update did not accept the working bytes: %+v", moved.After)
	}
}

func TestFileRevertNavigatesRecurringDigestsByParent(t *testing.T) {
	w := newWorkspace(t)
	w.writeManifest(t, baseManifest(t))
	alpha, beta := []byte("alpha\n"), []byte("beta\n")
	path := "config/rules.txt"
	w.write(t, path, alpha, 0o644)
	w.lucyOK(t, "file", "track", path)
	w.write(t, path, beta, 0o644)
	w.lucyOK(t, "file", "update", path)
	w.write(t, path, alpha, 0o644)
	w.lucyOK(t, "file", "update", path)

	events := w.lucyHistory(t, path)
	if len(events) != 3 {
		t.Fatalf("history holds %d events, want 3", len(events))
	}
	first, second, third := events[0], events[1], events[2]
	if c := requireChange(t, first, path); c.Revision != 1 || c.Parent != 0 || c.After.Hash != acceptedHash(alpha) {
		t.Errorf("unexpected first revision %+v", c)
	}
	if c := requireChange(t, second, path); c.Revision != 2 || c.Parent != 1 || c.After.Hash != acceptedHash(beta) {
		t.Errorf("unexpected second revision %+v", c)
	}
	if c := requireChange(t, third, path); c.Revision != 3 || c.Parent != 2 || c.After.Hash != acceptedHash(alpha) {
		t.Errorf("unexpected recurrent revision %+v", c)
	}

	// The accepted revision's parent is followed, so a recurrent digest keeps
	// its own identity instead of toggling between two contents.
	moved := w.fileResult(t, "revert", path)
	if len(moved.Changes) != 1 {
		t.Fatalf("unexpected revert result %+v", moved)
	}
	if moved.Changes[0].Revision != 2 {
		t.Errorf("default revert selected revision %d, want 2", moved.Changes[0].Revision)
	}
	w.requireBytes(t, path, beta)
	if declared := w.readManifest(t).Files[0]; declared.Hash != acceptedHash(beta) {
		t.Errorf("revert accepted %s, want the parent revision", declared.Hash)
	}

	w.fileResult(t, "revert", path)
	w.requireBytes(t, path, alpha)
	if declared := w.readManifest(t).Files[0]; declared.Hash != acceptedHash(alpha) {
		t.Errorf("second revert accepted %s, want the grandparent revision", declared.Hash)
	}

	events = w.lucyHistory(t, path)
	if len(events) != 5 {
		t.Fatalf("history holds %d events after two reverts, want 5", len(events))
	}
	for _, event := range events[3:] {
		change := requireChange(t, event, path)
		if change.Accepted {
			t.Errorf("revert %d was recorded as an accepted revision", event.Sequence)
		}
	}
}

func TestFileRevertTargetsSelectRevisionAndSequence(t *testing.T) {
	w := newWorkspace(t)
	w.writeManifest(t, baseManifest(t))
	alpha, beta := []byte("alpha\n"), []byte("beta\n")
	path := "config/rules.txt"
	w.write(t, path, alpha, 0o644)
	w.lucyOK(t, "file", "track", path)
	w.write(t, path, beta, 0o644)
	w.lucyOK(t, "file", "update", path)
	w.write(t, path, alpha, 0o644)
	w.lucyOK(t, "file", "update", path)

	moved := w.fileResult(t, "revert", path, acceptedHash(alpha))
	if len(moved.Changes) != 1 || moved.Changes[0].Revision != 3 {
		t.Fatalf("hash targeting did not select the latest recurrent revision: %+v", moved)
	}
	w.requireBytes(t, path, alpha)
	if declared := w.readManifest(t).Files[0]; declared.Hash != acceptedHash(alpha) {
		t.Errorf("hash revert accepted %s", declared.Hash)
	}

	w.fileResult(t, "revert", path, "--revision", "2")
	w.requireBytes(t, path, beta)
	if declared := w.readManifest(t).Files[0]; declared.Hash != acceptedHash(beta) {
		t.Errorf("sequence revert accepted %s", declared.Hash)
	}

	beforeManifest := w.read(t, document.ManifestFilename)
	beforeHistory := w.read(t, ".lucy/history/files.jsonl")
	w.lucyFail(t, "file", "revert", path, acceptedHash(alpha), "--revision", "2")
	w.lucyFail(t, "file", "revert", path, "--revision", "99")
	w.lucyFail(t, "file", "revert", path, acceptedHash([]byte("never accepted")))
	w.lucyFail(t, "file", "revert", path, "--revision", "0")
	w.requireBytes(t, path, beta)
	w.requireBytes(t, document.ManifestFilename, beforeManifest)
	w.requireBytes(t, ".lucy/history/files.jsonl", beforeHistory)

	fresh := "config/fresh.txt"
	w.write(t, fresh, []byte("only revision\n"), 0o644)
	w.lucyOK(t, "file", "track", fresh)
	w.lucyFail(t, "file", "revert", fresh)
	w.requireBytes(t, fresh, []byte("only revision\n"))
}

func TestFileLockAndUnlockControlDriftWarnings(t *testing.T) {
	w := newWorkspace(t)
	w.writeManifest(t, baseManifest(t))
	source := newSourceServer(t, []byte("published rules\n"))
	first := []byte("rules v1\n")
	w.write(t, "config/rules.txt", first, 0o644)
	w.fileResult(t, "track", "config/rules.txt", "--recover-url", source.url("rules.txt"))

	locked := w.fileResult(t, "lock", "config/rules.txt")
	if len(locked.Changes) != 1 {
		t.Fatalf("unexpected lock result %+v", locked)
	}
	change := locked.Changes[0]
	if change.After == nil || !change.After.Immutable || change.After.Hash != acceptedHash(first) ||
		change.After.RecoverURL != source.url("rules.txt") {
		t.Errorf("lock changed the declaration: %+v", change.After)
	}

	drifted := []byte("drifted rules\n")
	w.write(t, "config/rules.txt", drifted, 0o644)
	status := w.statusOf(t, "config/rules.txt")
	if status.State != "modified" || !status.Immutable {
		t.Errorf("locked drift is not reported as a modified immutable file: %+v", status)
	}
	if status.Hash != acceptedHash(first) || status.ActualHash != acceptedHash(drifted) {
		t.Errorf("drift hides one of the two digests: %+v", status)
	}
	w.lucyOK(t, "file", "list")

	unlocked := w.fileResult(t, "unlock", "config/rules.txt")
	if len(unlocked.Changes) != 1 {
		t.Fatalf("unexpected unlock result %+v", unlocked)
	}
	if unlocked.Changes[0].After == nil || unlocked.Changes[0].After.Immutable {
		t.Errorf("unlock kept the immutable policy: %+v", unlocked.Changes[0].After)
	}
	if declared := w.readManifest(t).Files[0]; declared.Immutable || declared.Hash != acceptedHash(first) ||
		declared.RecoverURL != source.url("rules.txt") {
		t.Errorf("unlock lost the hash or the recovery URL: %+v", declared)
	}

	w.write(t, "config/rules.txt", []byte("mutable drift\n"), 0o644)
	w.lucyOK(t, "file", "list")
	if status := w.statusOf(t, "config/rules.txt"); status.State != "modified" || status.Immutable {
		t.Errorf("mutable drift is not reported plainly: %+v", status)
	}
}

func TestFileListIsReadOnlyAndKeepsUntrackedEvents(t *testing.T) {
	w := newWorkspace(t)
	w.writeManifest(t, baseManifest(t))
	w.write(t, "config/rules.txt", []byte("rules\n"), 0o644)
	w.lucyOK(t, "file", "track", "config/rules.txt")
	w.lucyOK(t, "file", "untrack", "config/rules.txt")
	w.requireBytes(t, "config/rules.txt", []byte("rules\n"))
	w.requireCASObject(t, digestHex([]byte("rules\n")), []byte("rules\n"))

	if len(w.readManifest(t).Files) != 0 {
		t.Error("untrack left a declaration behind")
	}
	events := w.lucyHistory(t, "config/rules.txt")
	if len(events) != 2 {
		t.Fatalf("history of an untracked path holds %d events, want 2", len(events))
	}
	if events[0].Operation != "track" {
		t.Errorf("history forgot the track event: %+v", events[0])
	}
	w.lucyOK(t, "file", "list")

	readonly := newWorkspace(t)
	readonly.writeManifest(t, baseManifest(t))
	if statuses := readonly.fileList(t); len(statuses) != 0 {
		t.Errorf("read-only list invented %d statuses", len(statuses))
	}
	if events := readonly.lucyHistory(t, ""); len(events) != 0 {
		t.Errorf("read-only history invented %d events", len(events))
	}
	readonly.lucyOK(t, "file", "history")
	readonly.requireAbsent(t, ".lucy")
}

func TestFileRecoverPrefersLocalCASBeforeURL(t *testing.T) {
	w := newWorkspace(t)
	w.writeManifest(t, baseManifest(t))
	source := newSourceServer(t, []byte("published rules\n"))
	rules := []byte("accepted rules\n")
	w.write(t, "config/rules.txt", rules, 0o644)
	w.fileResult(t, "track", "config/rules.txt", "--recover-url", source.url("rules.txt"))
	drifted := []byte("drifted rules\n")
	w.write(t, "config/rules.txt", drifted, 0o644)

	w.lucyOK(t, "file", "recover", "config/rules.txt")
	w.requireBytes(t, "config/rules.txt", rules)
	if declared := w.readManifest(t).Files[0]; declared.Hash != acceptedHash(rules) {
		t.Errorf("recover moved the accepted hash to %s", declared.Hash)
	}
	if hits := source.hits(); hits != 0 {
		t.Errorf("recover fetched the URL %d times although the local blob answered", hits)
	}
	events := w.lucyHistory(t, "")
	if len(events) != 2 || events[1].Operation != "recover" {
		t.Fatalf("history does not record the recovery: %+v", events)
	}
	if change := requireChange(t, events[1], "config/rules.txt"); change.BackupHash != acceptedHash(drifted) {
		t.Errorf("recovery lost the displaced working bytes: %+v", change)
	}
	w.requireCASObject(t, digestHex(drifted), drifted)
}

func TestFileRecoverKeepsMismatchedTargetAndDeclaration(t *testing.T) {
	w := newWorkspace(t)
	w.writeManifest(t, baseManifest(t))
	source := newSourceServer(t, []byte("published rules\n"))
	rules := []byte("accepted rules\n")
	w.write(t, "config/rules.txt", rules, 0o644)
	w.fileResult(t, "track", "config/rules.txt", "--recover-url", source.url("rules.txt"))
	drifted := []byte("drifted rules\n")
	w.write(t, "config/rules.txt", drifted, 0o644)
	w.dropCASObject(t, digestHex(rules))
	source.publish(t, []byte("rotated upstream rules\n"))
	beforeHistory := w.read(t, ".lucy/history/files.jsonl")

	w.lucyFail(t, "file", "recover", "config/rules.txt")
	w.requireBytes(t, "config/rules.txt", drifted)
	if declared := w.readManifest(t).Files[0]; declared.Hash != acceptedHash(rules) {
		t.Errorf("a failed recovery moved the accepted hash to %s", declared.Hash)
	}
	w.requireBytes(t, ".lucy/history/files.jsonl", beforeHistory)
	if hits := source.hits(); hits != 1 {
		t.Errorf("recovery fetched the URL %d times, want exactly one attempt", hits)
	}
}

func TestFileUpdateAcceptsBytesAndRetainsBackup(t *testing.T) {
	w := newWorkspace(t)
	w.writeManifest(t, baseManifest(t))
	first, second := []byte("published rules 1\n"), []byte("published rules 2\n")
	source := newSourceServer(t, first)
	working := []byte("working rules 1\n")
	w.write(t, "config/rules.txt", working, 0o644)
	w.fileResult(t, "track", "config/rules.txt", "--recover-url", source.url("rules.txt"))
	source.publish(t, second)

	w.lucyOK(t, "file", "update", "config/rules.txt", "--from-source")
	events := w.lucyHistory(t, "")
	if len(events) != 2 {
		t.Fatalf("history holds %d events after the source update, want 2", len(events))
	}
	change := requireChange(t, events[1], "config/rules.txt")
	if !change.Accepted || change.Revision != 2 || change.Parent != 1 {
		t.Errorf("unexpected source update lineage %+v", change)
	}
	if change.After == nil || change.After.Hash != acceptedHash(second) {
		t.Errorf("source update did not accept the published bytes: %+v", change.After)
	}
	if change.BackupHash != acceptedHash(working) {
		t.Errorf("source update lost the displaced working bytes: %+v", change)
	}
	w.requireBytes(t, "config/rules.txt", second)
	w.requireCASObject(t, digestHex(working), working)
	if declared := w.readManifest(t).Files[0]; declared.Hash != acceptedHash(second) ||
		declared.RecoverURL != source.url("rules.txt") {
		t.Errorf("source update rewrote the declaration: %+v", declared)
	}

	third := []byte("published rules 3\n")
	source.publish(t, third)
	fresh := w.fileResult(t, "update", "config/rules.txt", "--from-source")
	if len(fresh.Changes) != 1 || fresh.Changes[0].After == nil ||
		fresh.Changes[0].After.Hash != acceptedHash(third) {
		t.Errorf("stale bytes kept the source update back: %+v", fresh.Changes)
	}
	w.requireBytes(t, "config/rules.txt", third)
	if declared := w.readManifest(t).Files[0]; declared.Hash != acceptedHash(third) {
		t.Errorf("stale bytes kept the source update back: %s", declared.Hash)
	}
	if hits := source.hits(); hits != 2 {
		t.Errorf("source update fetched the URL %d times, want two fresh fetches", hits)
	}
}

func TestFileUpdateAllFailsAtomicallyThenSucceeds(t *testing.T) {
	w := newWorkspace(t)
	w.writeManifest(t, baseManifest(t))
	oldA, newA, oldB := []byte("rules a v1\n"), []byte("rules a v2\n"), []byte("rules b v1\n")
	w.write(t, "config/a.txt", oldA, 0o644)
	w.write(t, "config/b.txt", oldB, 0o644)
	w.lucyOK(t, "file", "track", "config/a.txt")
	w.lucyOK(t, "file", "track", "config/b.txt")
	w.write(t, "config/a.txt", newA, 0o644)
	if err := os.Remove(w.path("config/b.txt")); err != nil {
		t.Fatalf("remove tracked file: %v", err)
	}

	manifestBytes := w.read(t, document.ManifestFilename)
	w.lucyFail(t, "file", "update", "--all")
	w.requireBytes(t, document.ManifestFilename, manifestBytes)
	w.requireBytes(t, "config/a.txt", newA)
	if events := w.persistedHistory(t); len(events) != 2 {
		t.Fatalf("a failed bulk update left %d history events, want 2", len(events))
	}
	if status := w.statusOf(t, "config/a.txt"); status.State != "modified" {
		t.Errorf("the failed bulk update accepted the other file anyway: %+v", status)
	}

	w.write(t, "config/b.txt", oldB, 0o644)
	result := w.fileResult(t, "update", "--all")
	if len(result.Changes) != 2 {
		t.Fatalf("bulk update returned %d changes, want 2", len(result.Changes))
	}
	for _, change := range result.Changes {
		if !change.Accepted || change.Revision != 3 {
			t.Errorf("bulk update change is not an accepted revision: %+v", change)
		}
	}
	doc := w.readManifest(t)
	if len(doc.Files) != 2 {
		t.Fatalf("manifest declares %d files after the bulk update", len(doc.Files))
	}
	hashes := map[string]string{}
	for _, file := range doc.Files {
		hashes[file.Path] = file.Hash
	}
	if hashes["config/a.txt"] != acceptedHash(newA) || hashes["config/b.txt"] != acceptedHash(oldB) {
		t.Errorf("bulk update accepted the wrong bytes: %+v", hashes)
	}
	if status := w.statusOf(t, "config/a.txt"); status.State != "matching" {
		t.Errorf("bulk update left the workspace drifting: %+v", status)
	}
}

func TestFileRecoverAllSkipsPresenceOnlyEntries(t *testing.T) {
	w := newWorkspace(t)
	w.writeManifest(t, baseManifest(t))
	rules := []byte("accepted rules\n")
	w.write(t, "config/a.txt", rules, 0o644)
	w.write(t, "config/present.bin", []byte("marker\n"), 0o644)
	w.lucyOK(t, "file", "track", "config/a.txt")
	w.fileResult(t, "track", "config/present.bin", "--no-hash")
	w.write(t, "config/a.txt", []byte("drifted\n"), 0o644)

	result := w.fileResult(t, "recover", "--all")
	if len(result.Skipped) != 1 || result.Skipped[0] != "config/present.bin" {
		t.Fatalf("bulk recovery skipped %v", result.Skipped)
	}
	if len(result.Changes) != 1 || result.Changes[0].Path != "config/a.txt" {
		t.Fatalf("bulk recovery restored %+v", result.Changes)
	}
	w.requireBytes(t, "config/a.txt", rules)
	w.requireBytes(t, "config/present.bin", []byte("marker\n"))
	if status := w.statusOf(t, "config/a.txt"); status.Hash != acceptedHash(rules) || status.State != "matching" {
		t.Errorf("bulk recovery changed the accepted digest: %+v", status)
	}
}

func TestFileUpdateAllFromSourceSkipsEntriesWithoutURL(t *testing.T) {
	w := newWorkspace(t)
	w.writeManifest(t, baseManifest(t))
	published := []byte("published rules\n")
	source := newSourceServer(t, published)
	w.write(t, "config/a.txt", []byte("working a\n"), 0o644)
	w.write(t, "config/b.txt", []byte("working b\n"), 0o644)
	w.lucyOK(t, "file", "track", "config/a.txt", "--recover-url", source.url("a.txt"))
	w.lucyOK(t, "file", "track", "config/b.txt")
	source.publish(t, []byte("fresh published rules\n"))

	result := w.fileResult(t, "update", "--all", "--from-source")
	if len(result.Skipped) != 1 || result.Skipped[0] != "config/b.txt" {
		t.Fatalf("bulk source update skipped %v", result.Skipped)
	}
	if len(result.Changes) != 1 || result.Changes[0].Path != "config/a.txt" {
		t.Fatalf("bulk source update accepted %+v", result.Changes)
	}
	w.requireBytes(t, "config/a.txt", []byte("fresh published rules\n"))
	w.requireBytes(t, "config/b.txt", []byte("working b\n"))
	if status := w.statusOf(t, "config/a.txt"); status.Hash != acceptedHash([]byte("fresh published rules\n")) || status.State != "matching" {
		t.Errorf("bulk source update did not accept the replacement bytes: %+v", status)
	}
	if declared := w.readManifest(t).Files; len(declared) != 2 {
		t.Errorf("manifest holds %d declarations after the skipped bulk update", len(declared))
	}
}

func TestFileTrackNoHashTracksPresenceOnly(t *testing.T) {
	w := newWorkspace(t)
	w.writeManifest(t, baseManifest(t))
	marker := []byte("marker\n")
	w.write(t, "config/present.bin", marker, 0o644)

	result := w.fileResult(t, "track", "config/present.bin", "--no-hash")
	if len(result.Changes) != 1 {
		t.Fatalf("unexpected track result %+v", result)
	}
	if change := result.Changes[0]; change.After == nil || change.After.Hash != "" {
		t.Errorf("presence-only tracking recorded an accepted hash: %+v", change.After)
	}
	status := w.statusOf(t, "config/present.bin")
	if status.State != "presence-only" || status.Hash != "" {
		t.Errorf("unexpected presence-only status %+v", status)
	}
	w.lucyOK(t, "file", "list")
	if declared := w.readManifest(t).Files[0]; declared.Hash != "" {
		t.Errorf("manifest recorded an accepted hash for a presence-only file: %+v", declared)
	}

	w.lucyFail(t, "file", "recover", "config/present.bin")
	w.requireBytes(t, "config/present.bin", []byte("marker\n"))

	replaced := []byte("marker v2\n")
	w.write(t, "config/present.bin", replaced, 0o644)
	w.fileResult(t, "update", "config/present.bin")
	if status := w.statusOf(t, "config/present.bin"); status.State != "matching" || status.Hash != acceptedHash(replaced) {
		t.Errorf("update did not accept bytes for a previously presence-only file: %+v", status)
	}
	if declared := w.readManifest(t).Files[0]; declared.Path != "config/present.bin" ||
		declared.Hash != acceptedHash(replaced) || declared.Immutable {
		t.Errorf("unexpected declaration after accepting bytes: %+v", declared)
	}
}
