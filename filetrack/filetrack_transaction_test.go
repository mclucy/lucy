//go:build integration

package filetrack

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mclucy/lucy/cas"
	"github.com/mclucy/lucy/catalog"
	documentpkg "github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/fsutil"
	"github.com/mclucy/lucy/internal/workspacewrite"
	serverpkg "github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/workspace"
	"gopkg.in/yaml.v3"
)

func TestInterruptedFileTransactionRecovery(t *testing.T) {
	for _, test := range []struct{ name, mode string }{{"uncommitted partial application", "partial"}, {"committed completion", "committed"}} {
		t.Run(test.name, func(t *testing.T) {
			mode := test.mode
			t.Parallel()
			root := transactionWorkspace(t)
			original := snapshotTransactionFiles(t, root)
			modes := map[string]os.FileMode{}
			for _, name := range []string{"a.cfg", "b.cfg"} {
				info, err := os.Stat(filepath.Join(root, name))
				requireTransaction(t, err)
				modes[name] = info.Mode().Perm()
			}
			locked, err := documentpkg.LockFile(root).Read()
			requireTransaction(t, err)
			stop := startTransactionProcess(t, root, mode)
			stop()
			if _, err := History(root, ""); err == nil {
				t.Fatal("read-only history accepted a pending transaction")
			}
			_, err = Run(t.Context(), root, Request{Operation: Unlock, Paths: []string{"a.cfg"}})
			requireTransaction(t, err)
			document, err := documentpkg.ManifestFile(root).Read()
			requireTransaction(t, err)
			current, err := documentpkg.LockFile(root).Read()
			requireTransaction(t, err)
			if !reflect.DeepEqual(current.Server, locked.Server) || !reflect.DeepEqual(current.Packages, locked.Packages) {
				t.Fatal("recovery changed runtime pins")
			}
			if !reflect.DeepEqual(current.Files, document.Files) {
				t.Fatalf("recovery left lock file requirements different from intent: %+v versus %+v", current.Files, document.Files)
			}
			for _, name := range []string{"a.cfg", "b.cfg"} {
				wanted := original[name]
				if mode == "committed" {
					wanted = []byte("accepted-" + name)
				}
				requireTransactionBytes(t, filepath.Join(root, name), wanted)
				found := false
				for _, file := range document.Files {
					if file.Path == name {
						found = true
						if file.Hash != transactionDigest(wanted) {
							t.Fatalf("%s accepted %s, wanted digest of %q", name, file.Hash, wanted)
						}
					}
				}
				if !found {
					t.Fatalf("lost declaration %s", name)
				}
				info, err := os.Stat(filepath.Join(root, name))
				requireTransaction(t, err)
				if info.Mode().Perm() != modes[name] {
					t.Fatalf("mode changed for %s: %v", name, info.Mode())
				}
			}
			events, err := History(root, "")
			requireTransaction(t, err)
			wantedEvents := 3
			if mode == "committed" {
				wantedEvents = 4
				if events[2].Operation != Update || !events[2].Changes[0].Accepted {
					t.Fatal("durable accepted event was lost")
				}
			}
			if len(events) != wantedEvents || events[len(events)-1].Operation != Unlock {
				t.Fatalf("unexpected committed history: %+v", events)
			}
			if _, err := os.Stat(filepath.Join(root, transactionName)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("pending record remains: %v", err)
			}
			recovered := snapshotTransactionFiles(t, root)
			writer, err := workspacewrite.Lock(root)
			requireTransaction(t, err)
			requireTransaction(t, RecoverPending(root))
			requireTransaction(t, writer.Close())
			for name, data := range recovered {
				requireTransactionBytes(t, filepath.Join(root, name), data)
			}
		})
	}
}

func TestRecoveryPreservesPostInterruptionEdits(t *testing.T) {
	t.Parallel()
	root := transactionWorkspace(t)
	original := snapshotTransactionFiles(t, root)
	stop := startTransactionProcess(t, root, "partial")
	stop()
	requireTransaction(t, os.WriteFile(filepath.Join(root, "b.cfg"), []byte("external edit"), 0o640))
	interrupted := snapshotTransactionFiles(t, root)
	journal, err := os.ReadFile(filepath.Join(root, transactionName))
	requireTransaction(t, err)
	if _, err := Run(t.Context(), root, Request{Operation: Unlock, Paths: []string{"a.cfg"}}); err == nil {
		t.Fatalf("expected protected third state, got %v", err)
	}
	for name, data := range interrupted {
		requireTransactionBytes(t, filepath.Join(root, name), data)
	}
	requireTransactionBytes(t, filepath.Join(root, transactionName), journal)
	requireTransaction(t, os.WriteFile(filepath.Join(root, "b.cfg"), original["b.cfg"], 0o640))
	_, err = Run(t.Context(), root, Request{Operation: Unlock, Paths: []string{"a.cfg"}})
	requireTransaction(t, err)
	for _, name := range []string{"a.cfg", "b.cfg", documentpkg.ManifestFilename, documentpkg.LockFilename} {
		requireTransactionBytes(t, filepath.Join(root, name), original[name])
	}
}

func TestFileHistoryTailRepairAndMiddleCorruption(t *testing.T) {
	t.Parallel()
	root := transactionWorkspace(t)
	path := filepath.Join(root, historyName)
	original, err := os.ReadFile(path)
	requireTransaction(t, err)
	incomplete := append(bytes.Clone(original), []byte(`{"sequence":3`)...)
	requireTransaction(t, os.WriteFile(path, incomplete, 0o600))
	if _, err := History(root, ""); err == nil {
		t.Fatal("read-only history silently repaired an incomplete event")
	}
	requireTransactionBytes(t, path, incomplete)
	_, err = Run(t.Context(), root, Request{Operation: Unlock, Paths: []string{"a.cfg"}})
	requireTransaction(t, err)
	events, err := History(root, "")
	requireTransaction(t, err)
	if len(events) != 3 || events[2].Sequence != 3 || events[2].Operation != Unlock {
		t.Fatalf("tail repair lost committed events: %+v", events)
	}
	repaired, err := os.ReadFile(path)
	requireTransaction(t, err)
	if !bytes.HasPrefix(repaired, original) {
		t.Fatal("tail repair rewrote committed history")
	}
	corrupt := append(bytes.Clone(original), []byte("{broken record}\n")...)
	corrupt = append(corrupt, repaired[len(original):]...)
	requireTransaction(t, os.WriteFile(path, corrupt, 0o600))
	before := snapshotTransactionFiles(t, root)
	if _, err := Run(t.Context(), root, Request{Operation: Update, Paths: []string{"a.cfg"}}); err == nil {
		t.Fatal("mutation ignored complete middle corruption")
	}
	for name, data := range before {
		requireTransactionBytes(t, filepath.Join(root, name), data)
	}
}

func TestPreparedBatchRejectsConcurrentWorkingEdit(t *testing.T) {
	t.Parallel()
	root := transactionWorkspace(t)
	var edited atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/b" && edited.CompareAndSwap(false, true) {
			if err := os.WriteFile(filepath.Join(root, "a.cfg"), []byte("external during preparation"), 0o640); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
		}
		_, _ = io.WriteString(w, "initial-"+strings.TrimPrefix(r.URL.Path, "/")+".cfg")
	}))
	defer server.Close()
	document, err := documentpkg.ManifestFile(root).Read()
	requireTransaction(t, err)
	for i := range document.Files {
		document.Files[i].RecoverURL = server.URL + "/" + strings.TrimSuffix(document.Files[i].Path, ".cfg")
	}
	requireTransaction(t, documentpkg.ManifestFile(root).Write(document))
	locked, err := documentpkg.LockFile(root).Read()
	requireTransaction(t, err)
	locked.Files = document.Files
	locked.ManifestFingerprint, err = document.Fingerprint()
	requireTransaction(t, err)
	requireTransaction(t, documentpkg.LockFile(root).Write(locked))
	before := snapshotTransactionFiles(t, root)
	if _, err := Run(t.Context(), root, Request{Operation: Update, All: true, FromSource: true}); err == nil {
		t.Fatalf("expected readset rejection, got %v", err)
	}
	requireTransactionBytes(t, filepath.Join(root, "a.cfg"), []byte("external during preparation"))
	for _, name := range []string{"b.cfg", documentpkg.ManifestFilename, documentpkg.LockFilename, historyName} {
		requireTransactionBytes(t, filepath.Join(root, name), before[name])
	}
	if _, err := os.Stat(filepath.Join(root, transactionName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed preparation published a transaction: %v", err)
	}
	requireTransaction(t, os.WriteFile(filepath.Join(root, "a.cfg"), before["a.cfg"], 0o640))
	result, err := Run(t.Context(), root, Request{Operation: Update, All: true, FromSource: true})
	requireTransaction(t, err)
	if result.Event.Operation != Update || len(result.Event.Changes) != 2 {
		t.Fatalf("valid preparation did not resume after removing the conflict: %+v", result)
	}
}

func TestPreparedBatchPreservesConcurrentManifestIntent(t *testing.T) {
	t.Parallel()
	root := transactionWorkspace(t)
	external := make(chan []byte, 1)
	var edited atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/b" && edited.CompareAndSwap(false, true) {
			document, err := documentpkg.ManifestFile(root).Read()
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			document.Files[0].Immutable = true
			if err := documentpkg.ManifestFile(root).Write(document); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			data, err := os.ReadFile(filepath.Join(root, documentpkg.ManifestFilename))
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			external <- data
		}
		_, _ = io.WriteString(w, "initial-"+strings.TrimPrefix(r.URL.Path, "/")+".cfg")
	}))
	defer server.Close()
	document, err := documentpkg.ManifestFile(root).Read()
	requireTransaction(t, err)
	for i := range document.Files {
		document.Files[i].RecoverURL = server.URL + "/" + strings.TrimSuffix(document.Files[i].Path, ".cfg")
	}
	requireTransaction(t, documentpkg.ManifestFile(root).Write(document))
	locked, err := documentpkg.LockFile(root).Read()
	requireTransaction(t, err)
	locked.Files = document.Files
	locked.ManifestFingerprint, err = document.Fingerprint()
	requireTransaction(t, err)
	requireTransaction(t, documentpkg.LockFile(root).Write(locked))
	before := snapshotTransactionFiles(t, root)
	if _, err := Run(t.Context(), root, Request{Operation: Update, All: true, FromSource: true}); err == nil {
		t.Fatalf("expected intent conflict, got %v", err)
	}
	select {
	case data := <-external:
		requireTransactionBytes(t, filepath.Join(root, documentpkg.ManifestFilename), data)
	default:
		t.Fatal("fixture did not commit its external manifest edit")
	}
	for _, name := range []string{"a.cfg", "b.cfg", documentpkg.LockFilename, historyName} {
		requireTransactionBytes(t, filepath.Join(root, name), before[name])
	}
	requireTransaction(t, os.WriteFile(filepath.Join(root, documentpkg.ManifestFilename), before[documentpkg.ManifestFilename], 0o600))
	result, err := Run(t.Context(), root, Request{Operation: Update, All: true, FromSource: true})
	requireTransaction(t, err)
	if result.Event.Operation != Update || len(result.Event.Changes) != 2 {
		t.Fatalf("valid intent did not resume after removing the conflict: %+v", result)
	}
}

func TestWorkspaceWriterExcludesIndependentProcess(t *testing.T) {
	t.Parallel()
	root := transactionWorkspace(t)
	original := snapshotTransactionFiles(t, root)
	stop := startTransactionProcess(t, root, "hold-workspace")
	_, err := Run(t.Context(), root, Request{Operation: Update, Paths: []string{"a.cfg"}})
	if !errors.Is(err, fsutil.ErrLocked) {
		t.Fatalf("another process did not own the workspace: %v", err)
	}
	for name, data := range original {
		requireTransactionBytes(t, filepath.Join(root, name), data)
	}
	stop()
	_, err = Run(t.Context(), root, Request{Operation: Update, Paths: []string{"a.cfg"}})
	requireTransaction(t, err)
	events, err := History(root, "")
	requireTransaction(t, err)
	if len(events) != 3 || events[2].Operation != Update {
		t.Fatal("writer did not resume after process exit")
	}
}

func TestRecoveryWaitsForDeclaredRunningServer(t *testing.T) {
	t.Parallel()
	root := transactionWorkspace(t)
	stop := startTransactionProcess(t, root, "partial")
	stop()
	native := filepath.Join(root, "native")
	requireTransaction(t, os.MkdirAll(filepath.Join(native, "world"), 0o700))
	file, err := os.Create(filepath.Join(native, "server.jar"))
	requireTransaction(t, err)
	jar := zip.NewWriter(file)
	entry, err := jar.Create("version.json")
	requireTransaction(t, err)
	_, err = io.WriteString(entry, `{"id":"1.21.4"}`)
	requireTransaction(t, err)
	requireTransaction(t, jar.Close())
	requireTransaction(t, file.Close())
	requireTransaction(t, os.WriteFile(filepath.Join(native, "server.properties"), []byte("level-name=world\n"), 0o600))
	stopWorld := startTransactionProcess(t, root, "hold-world")
	if !workspace.RunningAt(native) {
		t.Fatal("fixture did not expose a live native session lock")
	}
	before := snapshotTransactionFiles(t, root)
	if _, err := Run(t.Context(), root, Request{Operation: Unlock, Paths: []string{"a.cfg"}}); err == nil {
		t.Fatalf("recovery ignored a live server: %v", err)
	}
	for name, data := range before {
		requireTransactionBytes(t, filepath.Join(root, name), data)
	}
	if _, err := os.Stat(filepath.Join(root, transactionName)); err != nil {
		t.Fatal("activity guard removed the journal")
	}
	stopWorld()
	_, err = Run(t.Context(), root, Request{Operation: Unlock, Paths: []string{"a.cfg"}})
	requireTransaction(t, err)
	requireTransactionBytes(t, filepath.Join(root, "a.cfg"), []byte("initial-a.cfg"))
}

func TestProvisionKeepsConcurrentFile(t *testing.T) {
	t.Parallel()
	root := transactionWorkspace(t)
	store, err := cas.New(filepath.Join(root, ".lucy"))
	requireTransaction(t, err)
	defer store.Close()
	accepted := bytes.Repeat([]byte("accepted\n"), 4096)
	object, err := store.Put(bytes.NewReader(accepted), nil)
	requireTransaction(t, err)
	source, err := store.Open(object.Digest)
	requireTransaction(t, err)
	defer source.Close()
	files, err := os.OpenRoot(root)
	requireTransaction(t, err)
	defer files.Close()
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	staged := make(chan error, 1)
	go func() {
		staged <- fsutil.WriteRoot(files, "new.cfg", reader, fsutil.WriteOptions{Mode: 0o640})
		_ = reader.Close()
	}()
	_, err = io.CopyN(writer, source, 128)
	requireTransaction(t, err)
	requireTransaction(t, os.WriteFile(filepath.Join(root, "new.cfg"), []byte("created by another writer"), 0o640))
	_, copyErr := io.Copy(writer, source)
	requireTransaction(t, errors.Join(copyErr, source.Close(), writer.Close()))
	if err := <-staged; !errors.Is(err, os.ErrExist) {
		t.Fatalf("expected no-replace publication failure: %v", err)
	}
	requireTransactionBytes(t, filepath.Join(root, "new.cfg"), []byte("created by another writer"))
	verified, err := store.Open(object.Digest)
	requireTransaction(t, err)
	data, readErr := io.ReadAll(verified)
	requireTransaction(t, errors.Join(readErr, verified.Close()))
	if !bytes.Equal(data, accepted) {
		t.Fatal("working publication altered immutable CAS bytes")
	}
}

func TestFileTransactionProcess(t *testing.T) {
	mode := os.Getenv("LUCY_TRANSACTION_HELPER")
	if mode == "" {
		return
	}
	root := os.Getenv("LUCY_TRANSACTION_ROOT")
	var lock *fsutil.FileLock
	var err error
	if mode == "hold-world" {
		lock, err = fsutil.LockFile(filepath.Join(root, "native", "world", "session.lock"))
	} else {
		lock, err = workspacewrite.Lock(root)
	}
	requireTransaction(t, err)
	defer lock.Close()
	if mode != "hold-world" && mode != "hold-workspace" {
		requireTransaction(t, prepareInterruptedTransaction(root, mode))
	}
	fmt.Println("ready")
	_, err = os.Stdin.Read(make([]byte, 1))
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
}

func prepareInterruptedTransaction(root, mode string) (resultErr error) {
	files, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, files.Close()) }()
	store, err := cas.New(filepath.Join(root, ".lucy"))
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, store.Close()) }()
	events, size, err := readHistory(root, false)
	if err != nil {
		return err
	}
	document, err := documentpkg.ManifestFile(root).Read()
	if err != nil {
		return err
	}
	locked, err := documentpkg.LockFile(root).Read()
	if err != nil {
		return err
	}
	txn := transaction{HistorySize: size, Event: Event{Sequence: uint64(len(events) + 1), Time: time.Now().UTC(), ID: rand.Text(), Operation: Update}}
	for i := range document.Files {
		declaration := document.Files[i]
		before, err := capture(root, files, store, declaration.Path)
		if err != nil {
			return err
		}
		after, err := storeBytes(store, []byte("accepted-"+declaration.Path), os.FileMode(before.Mode))
		if err != nil {
			return err
		}
		document.Files[i].Hash = after.Hash
		current := document.Files[i]
		txn.Event.Changes = append(txn.Event.Changes, Change{Path: declaration.Path, Before: &declaration, After: &current, BackupHash: before.Hash, BackupMode: before.Mode, Revision: txn.Event.Sequence, Parent: currentRevision(events, declaration.Path, declaration.Hash).sequence, Accepted: true, Mode: after.Mode})
		txn.Mutations = append(txn.Mutations, mutation{Path: declaration.Path, Before: before, After: after})
	}
	locked.Files = document.Files
	locked.ManifestFingerprint, err = document.Fingerprint()
	if err != nil {
		return err
	}
	for _, item := range []struct {
		name  string
		value any
	}{{documentpkg.ManifestFilename, document}, {documentpkg.LockFilename, locked}} {
		before, err := capture(root, files, store, item.name)
		if err != nil {
			return err
		}
		data, err := yaml.Marshal(item.value)
		if err != nil {
			return err
		}
		after, err := storeBytes(store, data, os.FileMode(before.Mode))
		if err != nil {
			return err
		}
		txn.Mutations = append(txn.Mutations, mutation{Path: item.name, Before: before, After: after})
	}
	data, err := json.Marshal(txn)
	if err != nil {
		return err
	}
	if err := fsutil.WriteRoot(files, transactionName, bytes.NewReader(data), fsutil.WriteOptions{
		Mode:    0o600,
		Replace: true,
	}); err != nil {
		return err
	}
	if mode == "partial" {
		for _, i := range []int{0, 2} {
			change := txn.Mutations[i]
			if err := applyImage(files, store, change.Path, change.After, change.Before); err != nil {
				return err
			}
		}
		return nil
	}
	for _, change := range txn.Mutations {
		if err := applyImage(files, store, change.Path, change.After, change.Before); err != nil {
			return err
		}
	}
	if err := appendEvent(files, txn.Event); err != nil {
		return err
	}
	// Simulate a directory entry that did not survive while the durable commit receipt did.
	change := txn.Mutations[1]
	return applyImage(files, store, change.Path, change.Before, change.After)
}

func startTransactionProcess(t *testing.T, root, mode string) func() {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	t.Cleanup(cancel)
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestFileTransactionProcess$")
	command.Env = append(os.Environ(), "LUCY_TRANSACTION_HELPER="+mode, "LUCY_TRANSACTION_ROOT="+root, "GORACE=atexit_sleep_ms=0")
	input, err := command.StdinPipe()
	requireTransaction(t, err)
	output, err := command.StdoutPipe()
	requireTransaction(t, err)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	requireTransaction(t, command.Start())
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		_ = command.Process.Kill()
		_ = input.Close()
		if err := command.Wait(); err == nil {
			t.Errorf("helper exited without interruption (%s)", mode)
		}
	}
	t.Cleanup(stop)
	line, err := bufio.NewReader(output).ReadString('\n')
	if err != nil || line != "ready\n" {
		stop()
		t.Fatalf("helper %s readiness: %q, %v\n%s", mode, line, err, stderr.String())
	}
	return stop
}

func transactionWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	document := &documentpkg.Manifest{
		SchemaVersion: documentpkg.ManifestSchemaVersion,
		Server: documentpkg.ManifestServer{
			Minecraft: "1.21.4",
			Directory: "native",
			Core: documentpkg.ManifestCore{
				Product:        serverpkg.ProductMinecraft,
				ProductVersion: "1.21.4",
			},
		},
	}
	requireTransaction(t, documentpkg.ManifestFile(root).Write(document))
	fingerprint, err := document.Fingerprint()
	requireTransaction(t, err)
	coreHash := sha256.Sum256([]byte("native core"))
	locked := &documentpkg.Lock{
		SchemaVersion:       documentpkg.LockSchemaVersion,
		ManifestFingerprint: fingerprint,
		Server: documentpkg.LockedServer{
			Minecraft:   "1.21.4",
			Directory:   "native",
			Product:     serverpkg.ProductMinecraft,
			CoreVersion: "1.21.4",
			Java:        documentpkg.LockedJava{Advisory: 21},
			Artifact: catalog.Artifact{
				Provider: "mojang",
				Version:  "1.21.4",
				Filename: "server.jar",
				URL:      "https://example.invalid/native",
				Hashes:   map[string]string{"sha256": fmt.Sprintf("%x", coreHash)},
			},
		},
		Packages: []documentpkg.Package{},
	}
	requireTransaction(t, documentpkg.LockFile(root).Write(locked))
	for _, name := range []string{"a.cfg", "b.cfg"} {
		requireTransaction(t, os.WriteFile(filepath.Join(root, name), []byte("initial-"+name), 0o640))
		_, err := Run(t.Context(), root, Request{Operation: Track, Paths: []string{name}})
		requireTransaction(t, err)
	}
	return root
}

func transactionDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + fmt.Sprintf("%x", sum)
}

func snapshotTransactionFiles(t *testing.T, root string) map[string][]byte {
	t.Helper()
	result := map[string][]byte{}
	for _, name := range []string{"a.cfg", "b.cfg", documentpkg.ManifestFilename, documentpkg.LockFilename, historyName} {
		data, err := os.ReadFile(filepath.Join(root, name))
		requireTransaction(t, err)
		result[name] = data
	}
	return result
}

func requireTransaction(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func requireTransactionBytes(t *testing.T, path string, wanted []byte) {
	t.Helper()
	actual, err := os.ReadFile(path)
	requireTransaction(t, err)
	if !bytes.Equal(actual, wanted) {
		t.Fatalf("%s: got %q, want %q", path, actual, wanted)
	}
}
