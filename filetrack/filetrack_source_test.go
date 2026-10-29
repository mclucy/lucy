package filetrack

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mclucy/lucy/cas"
	documentpkg "github.com/mclucy/lucy/document"
)

func TestSourceImageRejectsMismatchedContents(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "served contents")
	}))
	t.Cleanup(server.Close)
	store, err := cas.New(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	file := documentpkg.TrackedFile{
		Path:       "config/rules.txt",
		Hash:       behaviorDigest([]byte("accepted contents")),
		RecoverURL: server.URL,
	}
	if _, err := sourceImage(t.Context(), server.Client(), store, file, false, false); err == nil {
		t.Fatal("sourceImage accepted contents that do not match the accepted digest")
	}
	count := 0
	if err := store.Walk(func(cas.Object) error {
		count++
		return nil
	}); err != nil {
		t.Fatalf("walk store after rejected recovery: %v", err)
	}
	if count != 0 {
		t.Fatalf("rejected recovery published %d objects", count)
	}
}

func TestSourceImageRequiresClientForRemoteSource(t *testing.T) {
	t.Parallel()
	store, err := cas.New(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	file := documentpkg.TrackedFile{
		Path:       "config/rules.txt",
		Hash:       behaviorDigest([]byte("accepted contents")),
		RecoverURL: "https://example.com/rules.txt",
	}
	if _, err := sourceImage(t.Context(), nil, store, file, false, false); err == nil ||
		!strings.Contains(err.Error(), "requires an HTTP client") {
		t.Fatalf("sourceImage without a client = %v", err)
	}
}

func TestCappedReaderRejectsOverflow(t *testing.T) {
	t.Parallel()
	reader := &cappedReader{reader: strings.NewReader("12345678"), left: 4}
	if _, err := io.ReadAll(reader); err == nil {
		t.Fatal("cappedReader accepted a stream over its budget")
	}
}
