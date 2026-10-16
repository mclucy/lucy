//go:build integration

package file_test

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/internal/fsutil"
	"github.com/mclucy/lucy/lockfile"
	"github.com/mclucy/lucy/manifest"
	"github.com/mclucy/lucy/types"
	"gopkg.in/yaml.v3"
)

// workspace is one hermetic server directory plus the private home every
// subprocess runs under, so no test can observe another test's state.
type workspace struct {
	root string
	home string
}

func newWorkspace(t *testing.T) workspace {
	t.Helper()
	root := t.TempDir()
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("resolve workspace path: %v", err)
	}
	home := t.TempDir()
	directories := []string{
		home,
		filepath.Join(home, "cache"),
		filepath.Join(home, "appdata"),
		filepath.Join(home, "tmp"),
	}
	for _, directory := range directories {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatalf("create private home %s: %v", directory, err)
		}
	}
	return workspace{root: root, home: home}
}

func (w workspace) path(elem ...string) string {
	return filepath.Join(append([]string{w.root}, elem...)...)
}

func (w workspace) write(t *testing.T, relative string, data []byte, mode os.FileMode) {
	t.Helper()
	target := w.path(relative)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("create parent of %s: %v", relative, err)
	}
	if err := os.WriteFile(target, data, mode); err != nil {
		t.Fatalf("write %s: %v", relative, err)
	}
}

func (w workspace) read(t *testing.T, relative string) []byte {
	t.Helper()
	data, err := os.ReadFile(w.path(relative))
	if err != nil {
		t.Fatalf("read %s: %v", relative, err)
	}
	return data
}

func (w workspace) exists(t *testing.T, relative string) bool {
	t.Helper()
	_, err := os.Lstat(w.path(relative))
	if err == nil {
		return true
	}
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	t.Fatalf("stat %s: %v", relative, err)
	return false
}

func (w workspace) requireAbsent(t *testing.T, relative string) {
	t.Helper()
	if w.exists(t, relative) {
		t.Fatalf("%s still exists after the operation", relative)
	}
}

func (w workspace) requireBytes(t *testing.T, relative string, want []byte) {
	t.Helper()
	got := w.read(t, relative)
	if !bytes.Equal(got, want) {
		t.Fatalf("%s holds %q, want %q", relative, got, want)
	}
}

func (w workspace) modTime(t *testing.T, relative string) time.Time {
	t.Helper()
	info, err := os.Lstat(w.path(relative))
	if err != nil {
		t.Fatalf("stat %s: %v", relative, err)
	}
	return info.ModTime()
}

func (w workspace) chmod(t *testing.T, relative string, mode os.FileMode) {
	t.Helper()
	if err := os.Chmod(w.path(relative), mode); err != nil {
		t.Fatalf("chmod %s: %v", relative, err)
	}
}

// requireMode proves the command left the permissions a user chose alone.
func (w workspace) requireMode(t *testing.T, relative string, want os.FileMode) {
	t.Helper()
	info, err := os.Lstat(w.path(relative))
	if err != nil {
		t.Fatalf("stat %s: %v", relative, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s holds mode %04o, want %04o", relative, got, want)
	}
}

func (w workspace) symlink(t *testing.T, target, relative string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(w.path(relative)), 0o755); err != nil {
		t.Fatalf("create parent of %s: %v", relative, err)
	}
	if err := os.Symlink(target, w.path(relative)); err != nil {
		t.Fatalf("symlink %s: %v", relative, err)
	}
}

func (w workspace) writeManifest(t *testing.T, doc *manifest.Document) string {
	t.Helper()
	if err := manifest.Normalize(doc); err != nil {
		t.Fatalf("fixture manifest is invalid: %v", err)
	}
	fingerprint, err := manifest.Fingerprint(doc)
	if err != nil {
		t.Fatalf("fingerprint fixture manifest: %v", err)
	}
	data, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal fixture manifest: %v", err)
	}
	w.write(t, manifest.Filename, data, 0o644)
	return fingerprint
}

func (w workspace) readManifest(t *testing.T) *manifest.Document {
	t.Helper()
	doc, err := manifest.Read(w.root)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	return doc
}

// manifestFingerprint is the fingerprint Lucy stores in a current lockfile.
func (w workspace) manifestFingerprint(t *testing.T) string {
	t.Helper()
	fingerprint, err := manifest.Fingerprint(w.readManifest(t))
	if err != nil {
		t.Fatalf("fingerprint manifest: %v", err)
	}
	return fingerprint
}

func (w workspace) writeLock(t *testing.T, doc *lockfile.Document) {
	t.Helper()
	if err := lockfile.Validate(doc); err != nil {
		t.Fatalf("fixture lock is invalid: %v", err)
	}
	data, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal fixture lock: %v", err)
	}
	w.write(t, lockfile.Filename, data, 0o644)
}

func (w workspace) readLock(t *testing.T) *lockfile.Document {
	t.Helper()
	doc, err := lockfile.Read(w.root)
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	return doc
}

type receiptEntry struct {
	Path   string `yaml:"path"`
	SHA256 string `yaml:"sha256"`
	Mode   uint32 `yaml:"mode"`
}

type receiptDocument struct {
	Version         int            `yaml:"version"`
	LockFingerprint string         `yaml:"lock_fingerprint"`
	Entries         []receiptEntry `yaml:"entries"`
}

// receiptProjection is the digest a fixture receipt must pin to stay current.
func receiptProjection(t *testing.T, lock *lockfile.Document) string {
	t.Helper()
	fingerprint, err := install.ResolutionFingerprint(lock)
	if err != nil {
		t.Fatalf("fingerprint lock resolution: %v", err)
	}
	return fingerprint
}

func (w workspace) writeReceipt(t *testing.T, lock *lockfile.Document, entries ...receiptEntry) {
	t.Helper()
	doc := receiptDocument{
		Version:         1,
		LockFingerprint: receiptProjection(t, lock),
		Entries:         entries,
	}
	data, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal fixture receipt: %v", err)
	}
	w.write(t, ".lucy/install.yaml", data, 0o644)
}

func (w workspace) readReceipt(t *testing.T) receiptDocument {
	t.Helper()
	var doc receiptDocument
	if err := yaml.Unmarshal(w.read(t, ".lucy/install.yaml"), &doc); err != nil {
		t.Fatalf("decode installation receipt: %v", err)
	}
	return doc
}

func (w workspace) requireReceiptEntries(t *testing.T, want ...receiptEntry) {
	t.Helper()
	doc := w.readReceipt(t)
	if len(doc.Entries) != len(want) {
		t.Fatalf("receipt holds %d entries, want %d", len(doc.Entries), len(want))
	}
	for _, entry := range want {
		found := false
		for _, actual := range doc.Entries {
			if actual.Path == entry.Path {
				found = true
				if actual.SHA256 != entry.SHA256 {
					t.Errorf("receipt %s pins %s, want %s", entry.Path, actual.SHA256, entry.SHA256)
				}
				if actual.Mode != entry.Mode {
					t.Errorf("receipt %s pins mode %d, want %d", entry.Path, actual.Mode, entry.Mode)
				}
			}
		}
		if !found {
			t.Errorf("receipt lost its entry for %s", entry.Path)
		}
	}
}

// seedNativeRuntime writes a manifest, a current lock and an installation
// receipt over real core bytes, so a following install is a no-op instead of
// a fresh resolve. The declared tracked files take part in the lock and the
// receipt stays pinned to the runtime only.
func (w workspace) seedNativeRuntime(t *testing.T, files ...types.TrackedFile) (*manifest.Document, *lockfile.Document, []byte) {
	t.Helper()
	doc := &manifest.Document{
		Version: manifest.FormatVersion,
		Server: manifest.Server{
			Minecraft: "1.21.4",
			Directory: "server",
			Core:      manifest.Core{Distribution: "vanilla"},
		},
		Files: files,
	}
	fingerprint := w.writeManifest(t, doc)
	core := []byte("miniature server core bytes\n")
	w.write(t, "server/server.jar", core, 0o644)
	lock := &lockfile.Document{
		Version:             lockfile.FormatVersion,
		ManifestFingerprint: fingerprint,
		Server: lockfile.Server{
			Minecraft:    "1.21.4",
			Directory:    "server",
			Distribution: "vanilla",
			CoreVersion:  "1.21.4",
			Artifact: types.Artifact{
				Provider: "vanilla",
				Version:  "1.21.4",
				Filename: "server.jar",
				URL:      "https://example.invalid/server.jar",
				Hashes:   map[string]string{"sha256": digestHex(core)},
			},
			Java: 21,
		},
		Packages: []types.LockedPackage{},
		Files:    files,
	}
	w.writeLock(t, lock)
	w.writeReceipt(t, lock, receiptEntry{Path: "server/server.jar", SHA256: digestHex(core), Mode: 0o644})
	return doc, lock, core
}

func casObjectName(hexDigest string) string {
	return filepath.Join(".lucy", "objects", "sha256", hexDigest[:2], hexDigest)
}

func (w workspace) requireCASObject(t *testing.T, hexDigest string, want []byte) {
	t.Helper()
	data, err := os.ReadFile(w.path(casObjectName(hexDigest)))
	if err != nil {
		t.Fatalf("read cas object %s: %v", hexDigest, err)
	}
	if !bytes.Equal(data, want) {
		t.Fatalf("cas object %s holds %q, want %q", hexDigest, data, want)
	}
}

func (w workspace) dropCASObject(t *testing.T, hexDigest string) {
	t.Helper()
	if err := os.Remove(w.path(casObjectName(hexDigest))); err != nil {
		t.Fatalf("drop cas object %s: %v", hexDigest, err)
	}
}

func digestHex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// writeFileBytes and mkdirIfMissing seed paths outside the workspace, which
// only a fixture helper may touch.
func writeFileBytes(target string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return os.WriteFile(target, data, mode)
}

func mkdirIfMissing(target string) error {
	return os.MkdirAll(target, 0o755)
}

func acceptedHash(data []byte) string {
	return "sha256:" + digestHex(data)
}

func manifestTrackedFile(path, hash string) types.TrackedFile {
	return types.TrackedFile{Path: path, Hash: hash}
}

// lucyRun is one subprocess outcome. stdout carries command output; warnings
// and logs arrive on stderr.
type lucyRun struct {
	stdout   string
	stderr   string
	exitCode int
}

func (w workspace) lucy(t *testing.T, args ...string) lucyRun {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	argv := append(append([]string{}, args...), "--no-style")
	command := exec.CommandContext(ctx, lucyBinary, argv...)
	command.Dir = w.root
	command.Env = lucySubprocessEnv(w.home)
	command.WaitDelay = 10 * time.Second
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	run := lucyRun{stdout: stdout.String(), stderr: stderr.String()}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr):
		run.exitCode = exitErr.ExitCode()
	default:
		t.Fatalf("run lucy %v: %v\nstdout:\n%s\nstderr:\n%s", args, err, run.stdout, run.stderr)
	}
	return run
}

func (w workspace) lucyOK(t *testing.T, args ...string) lucyRun {
	t.Helper()
	run := w.lucy(t, args...)
	if run.exitCode != 0 {
		t.Fatalf("lucy %v exited %d\nstdout:\n%s\nstderr:\n%s", args, run.exitCode, run.stdout, run.stderr)
	}
	return run
}

func (w workspace) lucyFail(t *testing.T, args ...string) lucyRun {
	t.Helper()
	run := w.lucy(t, args...)
	if run.exitCode == 0 {
		t.Fatalf("lucy %v unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", args, run.stdout, run.stderr)
	}
	return run
}

// lucySubprocessEnv keeps every subprocess inside a private home, cache and
// temporary root. The inherited environment is otherwise shared so tools the
// command needs stay reachable.
func lucySubprocessEnv(home string) []string {
	overrides := map[string]string{
		"HOME":           home,
		"XDG_CACHE_HOME": filepath.Join(home, "cache"),
		"LOCALAPPDATA":   filepath.Join(home, "appdata"),
		"TMPDIR":         filepath.Join(home, "tmp"),
		"GORACE":         "atexit_sleep_ms=0",
	}
	environment := make([]string, 0, len(os.Environ())+len(overrides))
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if _, shadowed := overrides[strings.ToUpper(key)]; shadowed {
			continue
		}
		environment = append(environment, entry)
	}
	for key, value := range overrides {
		environment = append(environment, key+"="+value)
	}
	return environment
}

func decodeJSON[T any](t *testing.T, raw string) T {
	t.Helper()
	var value T
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		t.Fatalf("decode lucy JSON output: %v\n%s", err, raw)
	}
	return value
}

type fileDeclarationView struct {
	Path       string `json:"path"`
	Immutable  bool   `json:"immutable"`
	Hash       string `json:"hash"`
	RecoverURL string `json:"recover_url"`
}

type fileChangeView struct {
	Path       string               `json:"path"`
	Before     *fileDeclarationView `json:"before"`
	After      *fileDeclarationView `json:"after"`
	BackupHash string               `json:"backup_hash"`
	Revision   uint64               `json:"revision"`
	Parent     uint64               `json:"parent"`
	Accepted   bool                 `json:"accepted"`
}

type fileResultView struct {
	Operation string           `json:"operation"`
	Changes   []fileChangeView `json:"changes"`
	Skipped   []string         `json:"skipped"`
}

type fileStatusView struct {
	Path       string `json:"path"`
	Immutable  bool   `json:"immutable"`
	Hash       string `json:"hash"`
	ActualHash string `json:"actual_hash"`
	State      string `json:"state"`
	RecoverURL string `json:"recover_url"`
	Error      string `json:"error"`
}

type historyChangeView struct {
	Path       string               `json:"path"`
	After      *fileDeclarationView `json:"after"`
	BackupHash string               `json:"backup_hash"`
	Revision   uint64               `json:"revision"`
	Parent     uint64               `json:"parent"`
	Accepted   bool                 `json:"accepted"`
}

type historyEventView struct {
	Sequence  uint64              `json:"sequence"`
	Operation string              `json:"operation"`
	Changes   []historyChangeView `json:"changes"`
}

type doctorReportView struct {
	Warnings []struct {
		Path    string `json:"path"`
		Message string `json:"message"`
	} `json:"warnings"`
	Failures []struct {
		Path    string `json:"path"`
		Message string `json:"message"`
	} `json:"failures"`
}

func (w workspace) fileResult(t *testing.T, args ...string) fileResultView {
	t.Helper()
	argv := make([]string, len(args)+2)
	argv[0] = "file"
	copy(argv[1:], args)
	argv[len(argv)-1] = "--json"
	run := w.lucyOK(t, argv...)
	return decodeJSON[fileResultView](t, run.stdout)
}

func (w workspace) fileList(t *testing.T) []fileStatusView {
	t.Helper()
	run := w.lucyOK(t, "file", "list", "--json")
	return decodeJSON[[]fileStatusView](t, run.stdout)
}

func (w workspace) statusOf(t *testing.T, path string) fileStatusView {
	t.Helper()
	for _, status := range w.fileList(t) {
		if status.Path == path {
			return status
		}
	}
	t.Fatalf("lucy file list does not declare %s", path)
	return fileStatusView{}
}

// lucyHistory runs the read-only history command and decodes its events.
func (w workspace) lucyHistory(t *testing.T, path string) []historyEventView {
	t.Helper()
	args := []string{"file", "history", "--json"}
	if path != "" {
		args = append(args, path)
	}
	run := w.lucyOK(t, args...)
	return decodeJSON[[]historyEventView](t, run.stdout)
}

// persistedHistory decodes the durable JSONL history directly, so sequence
// numbering is asserted against the bytes Lucy wrote rather than a rendering.
func (w workspace) persistedHistory(t *testing.T) []historyEventView {
	t.Helper()
	raw, err := os.ReadFile(w.path(".lucy", "history", "files.jsonl"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatalf("read persisted file history: %v", err)
	}
	events := []historyEventView{}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		event := decodeJSON[historyEventView](t, line)
		events = append(events, event)
	}
	return events
}

func changeFor(event historyEventView, path string) (historyChangeView, bool) {
	for _, change := range event.Changes {
		if change.Path == path {
			return change, true
		}
	}
	return historyChangeView{}, false
}

func requireChange(t *testing.T, event historyEventView, path string) historyChangeView {
	t.Helper()
	change, ok := changeFor(event, path)
	if !ok {
		t.Fatalf("history event #%d has no change for %s", event.Sequence, path)
	}
	return change
}

// sourceServer serves recovery bytes and counts the requests it answers, so
// tests prove when Lucy fetches a URL instead of using local storage.
type sourceServer struct {
	server   *httptest.Server
	mu       sync.Mutex
	content  []byte
	requests int
}

func newSourceServer(t *testing.T, initial []byte) *sourceServer {
	t.Helper()
	source := &sourceServer{content: initial}
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

func (s *sourceServer) url(name string) string {
	return s.server.URL + "/" + name
}

func (s *sourceServer) publish(t *testing.T, content []byte) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.content = content
}

func (s *sourceServer) hits() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests
}

// miniVanillaJar builds the smallest jar a runtime probe identifies as a
// Minecraft server: a Mojang version.json and nothing else.
func miniVanillaJar(t *testing.T, id string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	entry, err := writer.Create("version.json")
	if err != nil {
		t.Fatalf("create version.json entry: %v", err)
	}
	if _, err := entry.Write([]byte(`{"id":"` + id + `"}`)); err != nil {
		t.Fatalf("write version.json entry: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("seal miniature jar: %v", err)
	}
	return buffer.Bytes()
}

const (
	helperModeEnv = "LUCY_FILE_TEST_HELPER"
	helperLockEnv = "LUCY_FILE_TEST_LOCK"
)

// TestFileHelperProcess is the child mode of the helper process. A normal run
// skips it; the parent test starts this binary with a helper mode set.
func TestFileHelperProcess(t *testing.T) {
	mode := os.Getenv(helperModeEnv)
	if mode == "" {
		t.Skipf("process helper; enable it with %s", helperModeEnv)
	}
	switch mode {
	case "session-lock":
		runSessionLockHelper()
	default:
		t.Fatalf("unknown helper mode %q", mode)
	}
}

// runSessionLockHelper holds the real world session lock with the production
// locking primitive, announces readiness on stdout, and releases it when the
// parent closes stdin. The helper never waits on a clock: the lock is held
// exactly as long as the parent keeps the pipe open.
func runSessionLockHelper() {
	lock, err := fsutil.LockFile(os.Getenv(helperLockEnv))
	if err != nil {
		fmt.Fprintf(os.Stderr, "hold session lock: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("ready")
	if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
		fmt.Fprintf(os.Stderr, "await session lock release: %v\n", err)
		os.Exit(1)
	}
	if err := lock.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "release session lock: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}

// heldSessionLock is a helper process that owns the world session lock.
type heldSessionLock struct {
	command *exec.Cmd
	stdin   io.WriteCloser
	done    bool
}

// holdWorldSession starts the helper that locks <anchor>/world/session.lock,
// making the server directory report itself as running to every command.
func (w workspace) holdWorldSession(t *testing.T, anchor string) *heldSessionLock {
	t.Helper()
	lockPath := w.path(anchor, "world", "session.lock")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatalf("create world directory: %v", err)
	}
	if err := os.WriteFile(lockPath, nil, 0o644); err != nil {
		t.Fatalf("create session lock: %v", err)
	}
	command := exec.Command(os.Args[0], "-test.run=^TestFileHelperProcess$")
	command.Env = append(
		os.Environ(),
		helperModeEnv+"=session-lock",
		helperLockEnv+"="+lockPath,
		"GORACE=atexit_sleep_ms=0",
	)
	command.Dir = w.root
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatalf("hold helper stdin: %v", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatalf("capture helper output: %v", err)
	}
	command.Stderr = os.Stderr
	held := &heldSessionLock{command: command, stdin: stdin}
	t.Cleanup(held.release)
	if err := command.Start(); err != nil {
		t.Fatalf("start session lock helper: %v", err)
	}
	ready := make(chan string, 1)
	go func() {
		line, err := bufio.NewReader(stdout).ReadString('\n')
		if err != nil {
			ready <- ""
			return
		}
		ready <- line
	}()
	select {
	case line := <-ready:
		if strings.TrimSpace(line) != "ready" {
			held.release()
			t.Fatalf("session lock helper did not report readiness: %q", line)
		}
	case <-time.After(2 * time.Minute):
		held.release()
		t.Fatal("session lock helper did not start in time")
	}
	return held
}

func (h *heldSessionLock) release() {
	if h == nil || h.done {
		return
	}
	h.done = true
	// Closing stdin stops the helper, which releases the session lock it owns.
	if h.stdin != nil {
		_ = h.stdin.Close()
	}
	waited := make(chan error, 1)
	go func() { waited <- h.command.Wait() }()
	select {
	case <-waited:
	case <-time.After(30 * time.Second):
		if h.command.Process != nil {
			_ = h.command.Process.Kill()
		}
		<-waited
	}
}
