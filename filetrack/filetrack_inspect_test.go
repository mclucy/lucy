package filetrack

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mclucy/lucy/cas"
	documentpkg "github.com/mclucy/lucy/document"
)

func provisionWorkspace(t *testing.T) string {
	t.Helper()
	root := behaviorWorkspace(t)
	return root
}

func provisionStore(t *testing.T, root string, content []byte) string {
	t.Helper()
	store, err := cas.New(filepath.Join(root, ".lucy"))
	if err != nil {
		t.Fatalf("open workspace store: %v", err)
	}
	object, err := store.Put(bytes.NewReader(content), nil)
	if err != nil {
		_ = store.Close()
		t.Fatalf("store accepted contents: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close workspace store: %v", err)
	}
	return object.Digest.String()
}

func TestProvisionRestoresMissingAcceptedFileFromLocalStore(t *testing.T) {
	t.Parallel()
	root := provisionWorkspace(t)
	content := []byte("accepted rules\n")
	hash := provisionStore(t, root, content)
	diagnostics, err := NewService(nil, nil).Provision(
		t.Context(),
		root,
		[]documentpkg.TrackedFile{{Path: "config/rules.txt", Hash: hash}},
		true,
	)
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("Provision diagnostics = %+v", diagnostics)
	}
	if got := behaviorRead(t, root, "config/rules.txt"); !bytes.Equal(got, content) {
		t.Fatalf("provisioned contents = %q, want %q", got, content)
	}
	info, err := os.Stat(filepath.Join(root, "config/rules.txt"))
	if err != nil {
		t.Fatalf("stat provisioned file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("provisioned mode = %04o, want 0600", info.Mode().Perm())
	}
}

func TestProvisionPreservesPresentFilesAndReportsImmutableDrift(t *testing.T) {
	t.Parallel()
	accepted := []byte("accepted contents\n")
	drifted := []byte("working drift\n")
	hash := behaviorDigest(accepted)
	tests := []struct {
		name           string
		content        []byte
		immutable      bool
		wantDiagnostic bool
	}{
		{
			name:      "matching immutable",
			content:   accepted,
			immutable: true,
		},
		{
			name:           "drifted immutable",
			content:        drifted,
			immutable:      true,
			wantDiagnostic: true,
		},
		{
			name:      "drifted mutable",
			content:   drifted,
			immutable: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := provisionWorkspace(t)
			behaviorWrite(t, root, "config/rules.txt", test.content, 0o644)
			diagnostics, err := NewService(nil, nil).Provision(
				t.Context(),
				root,
				[]documentpkg.TrackedFile{{
					Path:      "config/rules.txt",
					Immutable: test.immutable,
					Hash:      hash,
				}},
				true,
			)
			if err != nil {
				t.Fatalf("Provision: %v", err)
			}
			if got := len(diagnostics) != 0; got != test.wantDiagnostic {
				t.Fatalf("diagnostics = %+v, want diagnostic=%v", diagnostics, test.wantDiagnostic)
			}
			if test.wantDiagnostic && !strings.Contains(diagnostics[0].Message, "immutable contents differ") {
				t.Fatalf("diagnostic = %q", diagnostics[0].Message)
			}
			if got := behaviorRead(t, root, "config/rules.txt"); !bytes.Equal(got, test.content) {
				t.Fatalf("Provision changed present contents to %q", got)
			}
		})
	}
}

func TestProvisionReportsMissingFileWithoutAcceptedHash(t *testing.T) {
	t.Parallel()
	root := provisionWorkspace(t)
	diagnostics, err := NewService(nil, nil).Provision(
		t.Context(),
		root,
		[]documentpkg.TrackedFile{{Path: "config/missing.txt"}},
		true,
	)
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Message, "no accepted hash") {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
	behaviorRequireAbsent(t, root, "config/missing.txt")
}

func TestProvisionReportsUnavailableAcceptedSource(t *testing.T) {
	t.Parallel()
	root := provisionWorkspace(t)
	hash := behaviorDigest([]byte("missing accepted contents\n"))
	diagnostics, err := NewService(nil, nil).Provision(
		t.Context(),
		root,
		[]documentpkg.TrackedFile{{Path: "config/rules.txt", Hash: hash}},
		true,
	)
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if len(diagnostics) != 1 ||
		!strings.Contains(diagnostics[0].Message, "no verified local backup or recovery source") {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
}

func TestProvisionFetchesAcceptedFileFromSource(t *testing.T) {
	t.Parallel()
	content := []byte("source rules\n")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(content)
	}))
	t.Cleanup(server.Close)
	root := provisionWorkspace(t)
	diagnostics, err := NewService(server.Client(), nil).Provision(
		t.Context(),
		root,
		[]documentpkg.TrackedFile{{
			Path:       "config/rules.txt",
			Hash:       behaviorDigest(content),
			RecoverURL: server.URL,
		}},
		false,
	)
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("Provision diagnostics = %+v", diagnostics)
	}
	if got := behaviorRead(t, root, "config/rules.txt"); !bytes.Equal(got, content) {
		t.Fatalf("provisioned contents = %q, want %q", got, content)
	}
}

func TestProvisionReportsSourceFailure(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "missing", http.StatusNotFound)
	}))
	t.Cleanup(server.Close)
	root := provisionWorkspace(t)
	diagnostics, err := NewService(server.Client(), nil).Provision(
		t.Context(),
		root,
		[]documentpkg.TrackedFile{{
			Path:       "config/rules.txt",
			Hash:       behaviorDigest([]byte("never served")),
			RecoverURL: server.URL,
		}},
		false,
	)
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Message, "HTTP 404") {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
}

func TestProvisionKeepsFileThatAppearsDuringRecovery(t *testing.T) {
	t.Parallel()
	content := []byte("accepted source contents\n")
	root := provisionWorkspace(t)
	appeared := []byte("created by another writer\n")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := os.MkdirAll(filepath.Join(root, "config"), 0o755); err != nil {
			t.Errorf("prepare concurrent file: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, "config/rules.txt"), appeared, 0o644); err != nil {
			t.Errorf("write concurrent file: %v", err)
		}
		_, _ = w.Write(content)
	}))
	t.Cleanup(server.Close)
	diagnostics, err := NewService(server.Client(), nil).Provision(
		t.Context(),
		root,
		[]documentpkg.TrackedFile{{
			Path:       "config/rules.txt",
			Hash:       behaviorDigest(content),
			RecoverURL: server.URL,
		}},
		false,
	)
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Message, "appeared during recovery") {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
	if got := behaviorRead(t, root, "config/rules.txt"); !bytes.Equal(got, appeared) {
		t.Fatalf("Provision overwrote the concurrent file with %q", got)
	}
}

func TestProvisionRejectsUnsafeState(t *testing.T) {
	t.Parallel()
	t.Run("non-regular path", func(t *testing.T) {
		t.Parallel()
		root := provisionWorkspace(t)
		if err := os.MkdirAll(filepath.Join(root, "config"), 0o755); err != nil {
			t.Fatalf("prepare directory: %v", err)
		}
		_, err := NewService(nil, nil).Provision(
			t.Context(),
			root,
			[]documentpkg.TrackedFile{{Path: "config"}},
			true,
		)
		if err == nil {
			t.Fatal("Provision accepted a directory as a tracked file")
		}
	})
	t.Run("pending transaction", func(t *testing.T) {
		t.Parallel()
		root := provisionWorkspace(t)
		if err := os.MkdirAll(filepath.Join(root, ".lucy"), 0o700); err != nil {
			t.Fatalf("prepare workspace state: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, transactionName), []byte("{}"), 0o600); err != nil {
			t.Fatalf("write pending transaction: %v", err)
		}
		_, err := NewService(nil, nil).Provision(
			t.Context(),
			root,
			[]documentpkg.TrackedFile{{Path: "config/rules.txt"}},
			true,
		)
		if err == nil {
			t.Fatal("Provision accepted a pending transaction")
		}
	})
	t.Run("canceled context", func(t *testing.T) {
		t.Parallel()
		root := provisionWorkspace(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := NewService(nil, nil).Provision(
			ctx,
			root,
			[]documentpkg.TrackedFile{{Path: "config/rules.txt"}},
			true,
		)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Provision with a canceled context = %v", err)
		}
	})
	t.Run("empty declarations", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		diagnostics, err := NewService(nil, nil).Provision(
			t.Context(),
			root,
			nil,
			false,
		)
		if err != nil || len(diagnostics) != 0 {
			t.Fatalf("Provision with no declarations = %+v, %v", diagnostics, err)
		}
		if _, err := os.Stat(filepath.Join(root, ".lucy")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("empty Provision created state: %v", err)
		}
	})
}

func TestDeclaredFilesPrefersManifest(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	hash := behaviorDigest([]byte("declared contents"))
	manifest := behaviorManifest()
	manifest.Files = []documentpkg.TrackedFile{{Path: "config/rules.txt", Hash: hash}}
	behaviorWriteManifest(t, root, manifest)
	files, err := DeclaredFiles(root)
	if err != nil {
		t.Fatalf("DeclaredFiles: %v", err)
	}
	if len(files) != 1 || files[0].Path != "config/rules.txt" || files[0].Hash != hash {
		t.Fatalf("DeclaredFiles = %+v", files)
	}
}

func TestDeclaredFilesRejectsMissingAndUnreadableState(t *testing.T) {
	t.Parallel()
	t.Run("both missing", func(t *testing.T) {
		t.Parallel()
		if _, err := DeclaredFiles(t.TempDir()); err == nil {
			t.Fatal("DeclaredFiles accepted a workspace without declarations")
		}
	})
	t.Run("unreadable manifest", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		if err := os.WriteFile(
			filepath.Join(root, documentpkg.ManifestFilename),
			[]byte("not a manifest"),
			0o600,
		); err != nil {
			t.Fatalf("write invalid manifest: %v", err)
		}
		if _, err := DeclaredFiles(root); err == nil {
			t.Fatal("DeclaredFiles accepted an unreadable manifest")
		}
	})
}

func TestInspectReportsObservedStates(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	matching := []byte("matching contents\n")
	modified := []byte("modified contents\n")
	behaviorWrite(t, root, "matching.txt", matching, 0o644)
	behaviorWrite(t, root, "modified.txt", modified, 0o644)
	behaviorWrite(t, root, "presence.txt", []byte("presence\n"), 0o644)
	declarations := []documentpkg.TrackedFile{
		{Path: "matching.txt", Hash: behaviorDigest(matching)},
		{Path: "modified.txt", Hash: behaviorDigest([]byte("accepted contents\n"))},
		{Path: "presence.txt"},
		{Path: "missing.txt", Hash: behaviorDigest([]byte("missing contents\n"))},
	}
	statuses, err := Inspect(root, declarations)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if len(statuses) != len(declarations) {
		t.Fatalf("Inspect returned %d statuses, want %d", len(statuses), len(declarations))
	}
	byPath := make(map[string]Status, len(statuses))
	for _, status := range statuses {
		byPath[status.Path] = status
	}
	if got := byPath["matching.txt"]; got.State != "matching" ||
		got.ActualHash != behaviorDigest(matching) {
		t.Fatalf("matching status = %+v", got)
	}
	if got := byPath["modified.txt"]; got.State != "modified" ||
		got.ActualHash != behaviorDigest(modified) {
		t.Fatalf("modified status = %+v", got)
	}
	if got := byPath["presence.txt"]; got.State != "presence-only" {
		t.Fatalf("presence status = %+v", got)
	}
	if got := byPath["missing.txt"]; got.State != "missing" {
		t.Fatalf("missing status = %+v", got)
	}
	if _, err := os.Stat(filepath.Join(root, ".lucy")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Inspect created state: %v", err)
	}
}
