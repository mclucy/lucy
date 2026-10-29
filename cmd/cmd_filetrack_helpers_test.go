package cmd

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

	documentpkg "github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/fsutil"
	serverpkg "github.com/mclucy/lucy/server"
	"gopkg.in/yaml.v3"
)

// baseManifest is the declaration every process-level file tracking case
// starts from.
func baseManifest(t *testing.T) *documentpkg.Manifest {
	t.Helper()
	return &documentpkg.Manifest{
		SchemaVersion: documentpkg.ManifestSchemaVersion,
		Server: documentpkg.ManifestServer{
			Minecraft: "1.21.4",
			Directory: "server",
			Core:      documentpkg.ManifestCore{Product: serverpkg.ProductMinecraft},
		},
	}
}

// workspace is one hermetic server directory plus the private home every
// invocation runs under, so no test can observe another test's state.
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
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "appdata"))
	t.Setenv("TMPDIR", filepath.Join(home, "tmp"))
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

func (w workspace) writeManifest(t *testing.T, doc *documentpkg.Manifest) string {
	t.Helper()
	if err := doc.Normalize(); err != nil {
		t.Fatalf("fixture manifest is invalid: %v", err)
	}
	fingerprint, err := doc.Fingerprint()
	if err != nil {
		t.Fatalf("fingerprint fixture manifest: %v", err)
	}
	data, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal fixture manifest: %v", err)
	}
	w.write(t, documentpkg.ManifestFilename, data, 0o644)
	return fingerprint
}

func (w workspace) writeLock(t *testing.T, doc *documentpkg.Lock) {
	t.Helper()
	if err := doc.Validate(); err != nil {
		t.Fatalf("fixture lock is invalid: %v", err)
	}
	data, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal fixture lock: %v", err)
	}
	w.write(t, documentpkg.LockFilename, data, 0o644)
}

func (w workspace) readLock(t *testing.T) *documentpkg.Lock {
	t.Helper()
	doc, err := documentpkg.LockFile(w.root).Read()
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	return doc
}

func digestHex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func acceptedHash(data []byte) string {
	return "sha256:" + digestHex(data)
}

// lucyRun is one invocation outcome. stdout carries command output; warnings
// and logs arrive on stderr.
type lucyRun struct {
	stdout   string
	stderr   string
	exitCode int
}

func (w workspace) lucy(t *testing.T, args ...string) lucyRun {
	t.Helper()
	argv := append(append([]string{}, args...), "--no-style")
	var stdout, stderr bytes.Buffer
	runner := NewRunner(argv, RunnerOptions{
		Directory:  w.root,
		Stdin:      strings.NewReader(""),
		Stdout:     &stdout,
		Stderr:     &stderr,
		HTTPClient: http.DefaultClient,
	})
	// Bound each invocation: the runner carries no deadline of its own, so a
	// stalled network operation would otherwise hold this test until the
	// package timeout expires.
	executeCtx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	executeErr := runner.Execute(executeCtx)
	closeErr := runner.Close()
	if executeErr == nil && closeErr == nil {
		executeErr = runner.FlushOutput()
	}
	run := lucyRun{stdout: stdout.String(), stderr: stderr.String()}
	if executeErr != nil || closeErr != nil {
		run.exitCode = 1
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
	document := decodeJSON[struct {
		Statuses []fileStatusView `json:"statuses"`
	}](t, run.stdout)
	return document.Statuses
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
	helperModeEnv = "LUCY_FILETRACK_TEST_HELPER"
	helperLockEnv = "LUCY_FILETRACK_TEST_LOCK"
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
